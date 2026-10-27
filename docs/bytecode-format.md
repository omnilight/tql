# Bytecode format, persistence, and tooling

Status: implemented on this branch. This document defines the binary image,
loader, disassembler and assembler that make TQL bytecode a durable artifact.

Related plans:

- [Bytecode virtual machine](bytecode-vm.md) — execution semantics and the
  AST-to-bytecode compiler.
- [Performance and SIMD](bytecode-performance.md) — the AST-versus-VM gate and
  SIMD suitability.

## Goals

- Save a compiled `Program` to a versioned, deterministic binary image.
- Load that image on another run, machine, or process without the TQL source.
- Print a readable disassembly that is also valid assembler input.
- Assemble hand-written or generated text into a `Program`.
- Reject corrupt or hostile images safely: clear errors, bounded work, no panics.
- Preserve exact program behavior: parameters, mutability, error text, step
  limits, scopes, references and transaction semantics.

Non-goals in the first version:

- Portable bytecode across incompatible TQL versions. The format is versioned and
  a mismatch is rejected.
- A stable ABI for Go internals, `syntax.Node`, formats, or `Document`.
- Embedding document data or the TQL source text in the image.
- A stepping debugger. Disassembly and optional execution tracing are the
  diagnostic tools.
- Compressed or encrypted images. These can be layered outside the format.

## Lessons from jq

jq was checked at master on 2026-10-27. Its in-memory `struct bytecode` holds a
`uint16_t` instruction array, a `jv` constants array, local/closure counts, a
subfunction table, a C-function table and `debuginfo` with function, parameter and
local names. `src/opcode_list.h` generates the opcode enum and the description
table used by the compiler, VM and disassembler. `--debug-dump-disasm` prints
program counters, mnemonics, branch targets, constants, variable names/levels and
subfunctions; `--debug-trace` prints executed instructions with stack values. A
post-compile peephole pass currently detects tail calls.

| Aspect | jq | TQL plan |
| --- | --- | --- |
| Execution model | Stack VM with fork/backtracking | Register VM with stream and scalar operands |
| Opcode metadata | `opcode_list.h` macro table | `internal/bytecode/opcodes.go` table |
| Code units | `uint16_t`, 16-bit operands | 32-bit operands plus explicit caps |
| Constants | `jv` array | Typed, versioned constant section |
| Debug names | `debuginfo` object | Parameter, slot, code and statement metadata |
| Disassembly | `--debug-dump-disasm` | `Program.Disassemble`, `--disassemble` |
| Persistence | None; in-memory only | `MarshalBinary` / `LoadProgram` |
| Assembler | None | `Assemble`, canonical round-trip |
| Optimization | Tail-call peephole | Later measured passes; none required in v1 |

We adopt the single opcode table, constants pool, sub-code table, debug names and
disassembler shape. We do not adopt backtracking, closures, 16-bit limits, or an
in-memory-only representation. jq's disassembly is a debug print; ours is a
canonical, re-assemblable form.

## Code model

`internal/bytecode` owns the code representation. It imports `internal/query`
only for AST step types and `syntax` for values; it never imports `tql`.

```go
type Code struct {
    Name      string   // debug name: main, predicate, selector, ...
    Registers int
    Ops       []Op
    Loops     []LoopInfo
    Scopes    []ScopeInfo
}

type Op struct {
    ID   uint8
    A, B, C, D int32
    Stmt int32 // statement index for error prefixes
}

type Program struct {
    Params []Param
    Consts []Value
    Slots  []string
    Paths  []PathPlan
    Targets []TargetPlan
    Codes  []*Code // index 0 is main
}
```

Opcode IDs are stable and append-only. One table owns the numeric ID, mnemonic,
operand layout and operand kinds:

```go
type OpInfo struct {
    ID       uint8
    Name     string
    Operands OperandLayout
}
```

The compiler, VM, verifier, disassembler and assembler read the same table. A
missing or unknown ID is a load error, never a silent aliasing of another
instruction. Operand fields are fixed-width (`int32`/`uint32`) so the image does
not depend on the host word size. Every code object returns its result in `r0`;
`main` ends with `HALT`.

Instructions carry a statement index for the `statement N:` error prefix. Slot
names are kept because dead references report `variable $name has no value`.
Parameters, defaults, loop/scope tables and sub-code names are part of the
program, not optional diagnostics.

## Binary format

### Header

All integers are little-endian. The header is fixed:

| Offset | Size | Field |
| ---: | ---: | --- |
| 0 | 4 | magic `TQLC` |
| 4 | 2 | format version, currently `1` |
| 6 | 2 | flags, reserved zero in v1 |
| 8 | 4 | header size, currently `24` |
| 12 | 4 | payload size |
| 16 | 4 | CRC32C (Castagnoli) of the payload |
| 20 | 4 | reserved, zero |
| 24 | ... | payload sections |

The checksum detects accidental corruption, not tampering. Validation is the
security boundary. A size that overflows, exceeds a limit, or disagrees with the
remaining input is rejected before allocation.

### Sections

The payload is a sequence of sections in a fixed order:

```text
section := id u16 | flags u16 | length u32 | data[length]
```

| ID | Section | Contents |
| ---: | --- | --- |
| 1 | `META` | Opcode-table/format version (`u32`) |
| 2 | `PARAM` | Parameter names, required/default flags, typed defaults |
| 3 | `CONST` | Typed constant pool |
| 4 | `SLOT` | Slot names, one per lexical slot |
| 5 | `PATH` | Compiled path plans and their step selectors/predicates |
| 6 | `TARGET` | Assignment/delete target plans |
| 7 | `TARGETSET` | Code lists used by `DELETE` |
| 8 | `ARGSET` | Register lists used by calls, objects, arrays and tree functions |
| 9 | `SCOPE` | Lexical slot ranges |
| 10 | `LOOP` | Loop descriptors |
| 11 | `CODE` | Code objects: main first, sub-codes addressed by index |

The current reader requires sections 1–11 in ascending order. A future `DEBUG`
section would receive a new ID and a format-version bump. Unknown sections are
rejected; duplicate or out-of-order sections are rejected.

Each code object stores its debug name, register count, instructions, loop
descriptors and scope ranges. Each instruction stores its opcode, operands and
statement index. Cross-references use indexes into `CONST`, `SLOT`, `PATH`,
`TARGET` and `CODE`.

### Values

Constants and parameter defaults use one typed encoding:

| Tag | Value | Payload |
| ---: | --- | --- |
| 0 | null | none |
| 1 | false | none |
| 2 | true | none |
| 3 | number | UTF-8 decimal lexeme, exact spelling retained |
| 4 | string | length + UTF-8 bytes |
| 5 | datetime | length + UTF-8 TOML date/time text |
| 6 | array | count + values |
| 7 | object | count + sorted key/value pairs |

Numbers are never converted through `float64`. Object keys are sorted at encode
time and must be unique. The decoder validates UTF-8, number syntax, datetime
syntax and nesting depth. `syntax.NormalizeValue` remains the authority for
accepted values.

### Determinism

`MarshalBinary` is deterministic:

- fixed section order, fixed field widths, little-endian;
- constants in index order, not map iteration order;
- object keys sorted, duplicate keys rejected;
- no pointers, Go type names, source paths or timestamps;
- debug comments never enter the binary.

Two runs of the same compiler version produce identical bytes. That makes golden
files, checksums and build reproducibility meaningful. The assembly text has the
same goal through canonical directive order and numeric instruction indexes.

### Versioning

The format version is independent of the library SemVer. Every change to layout,
opcode meaning, operand encoding or section semantics bumps it. `LoadProgram`
accepts only the version compiled into that build and reports:

```text
tql: bytecode format v2 is not supported by this build (supports v1); recompile the program
```

No implicit migration, no "best effort" decoding. Deployed programs should be
built and loaded by the same TQL version. Opcode IDs are never reused or
renumbered; new opcodes are appended and old ones stay retired.

### Limits

The loader enforces limits before allocating or walking untrusted data. Initial
values are intentionally generous for scripts and tiny compared with documents:

| Limit | Initial value |
| --- | ---: |
| Image size | 64 MiB |
| Code objects | 4,096 |
| Instructions total | 1,000,000 |
| Constants | 65,536 |
| Path plans | 65,536 |
| Target plans | 65,536 |
| Slots and registers | 65,536 each |
| Steps per path | 256 |
| Sub-code depth | 256 |
| Value nesting | 256 |

The limits are package constants, not user input. Raising one requires a format
version review and loader tests.

## Loading and validation

Loading is a read-only operation: it must not execute the program or touch a
document. The pipeline is:

1. check minimum length, magic, version, flags, header size and checksum;
2. read sections with bounded lengths and no arithmetic overflow;
3. decode values with depth/UTF-8/number checks;
4. decode code objects and cross-references;
5. run structural validation;
6. build an immutable `Program`.

The verifier checks at least:

- every register, slot, constant, path, target and code index is in range;
- every jump target is an instruction index inside its own code;
- paths have a known step kind and valid bounds/references;
- loops and scopes reference valid instructions and slots;
- sub-code references form a DAG (no cycles) within the depth limit;
- assignment operators and unary/binary operators are known;
- parameter names are valid, unique, and have either a default or required flag;
- code terminates with `HALT` and never falls through the end;
- the image has no trailing bytes and no duplicate sections.

Operand-kind mismatches (stream where scalar is required) are checked at
execution and reported as `invalid bytecode: ...`, never a panic. The VM bounds
checks every register and index even after verification, as defense in depth.
`Mutates()` is derived from `ASSIGN`/`DELETE` instructions, never read from the
file, so a loaded program cannot bypass `Query`'s read-only rule.

Malformed input must fail deterministically. The loader and assembler are fuzzed;
any panic is a bug, not a user error path.

## Text assembly

The assembly is line-oriented and canonical. It is both a debug view and an input
format; comments are ignored by the assembler.

### Syntax

- Lines beginning with `#` are comments. The disassembler does not emit trailing
  comments, so a `#` outside a quoted value is not special elsewhere.
- `.directive` lines configure the program or a code object.
- `MNEMONIC operand ...` is one instruction. Mnemonics come from the stable
  opcode table; malformed operand counts are assembly errors.
- Registers are numeric (`0`, `1`, ...); constants `K0`, ...; paths `P0`, ...;
  targets `T0`, ...; argument sets `A0`, ...; target sets `D0`, ...; codes
  `C0`, ...; loops `L0`, ...; scopes `S0`, ... .
- Jump operands are numeric instruction indexes inside the current code. The VM
  validates every index; labels are a readability improvement for a later
  version, not a requirement of v1.

Directives:

| Directive | Meaning |
| --- | --- |
| `.version N` | Must match the build's format version |
| `.param "name" required` | Required input parameter |
| `.param "name" default json <value>` | Optional input parameter with a default |
| `.const KN json <json>` | Value constant, using exact JSON numbers |
| `.const KN text "..."` | Text constant (key, name or operator) |
| `.const KN texts ["..."]` | Object key set |
| `.slot SN "name"` | Lexical slot name |
| `.path PN` | Start a path plan; following `.step` lines add steps |
| `.step key "k"`, `.step index N`, `.step wildcard`, `.step slice FROM TO`, `.step filter CN`, `.step dynamic CN` | One path step; `*` means an omitted slice bound |
| `.target TN path root=BOOL base=N path=PN` | Path target; `base=-1` means current input |
| `.target TN expr CN` | General expression target |
| `.argset AN [registers]` | Register list for calls, objects, arrays and tree functions |
| `.targetset DN [CN ...]` | Code list for `DELETE` |
| `.scope SN [slots]` | Lexical slot range |
| `.loop LN KIND continue=N break=N scope=N varslot=N` | Loop descriptor; KIND is `for` or `while` |
| `.code CN "name" REGS` | Start a code object; `C0` is main |
| `.stmt N` | Statement index for following instructions |
| `.end` | Optional end of a path/code/file |

Value constants use JSON syntax. Exact number spellings survive because the
decoder uses `json.Number` and converts it to `syntax.Number`; datetime constants
use `datetime "..."`. The binary format uses the typed encoding in
[Values](#values), not JSON.

### Example

This is the actual canonical output for
`services[name == "api"].replicas = 2`:

```asm
# TQL bytecode v1
.version 1
.const K0 json "api"
.const K1 text "=="
.const K2 text "="
.const K3 json 2
.path P0
.step key "name"
.path P1
.step key "services"
.step filter C1
.step key "replicas"
.target T0 path root=false base=-1 path=P1
.scope S0
.code C0 "main" 0
.stmt 1
STMT_ENTER 1
TICK
ASSIGN_CONST K2 T0 K3
STMT_LEAVE
HALT
.code C1 "filter" 4
.stmt 1
CTX 3
STEPS 3 3 P0
ONE 2 3
CONST_VALUE 3 K0
BINARY 1 2 3 K1
NODE 0 1
HALT
```

The example has no labels or free-form syntax: every directive maps directly to
a field in `Program`, and the assembler reconstructs the same image.

### Canonical disassembly

`Program.Disassemble` emits:

- header directives in fixed order (`.version`, parameters, constants, slots,
  paths, targets, sets, scopes, loops);
- code objects in index order, main first;
- instructions in index order with numeric register numbers and jump targets;
- `.stmt` only when the statement index changes;
- no comments in the canonical body; debug annotations may be added in a later
  version without changing the grammar.

## Public API and CLI

Go API:

```go
// MarshalBinary returns the deterministic, versioned image.
func (p *Program) MarshalBinary() ([]byte, error)

// LoadProgram validates and loads an image produced by MarshalBinary.
func LoadProgram(data []byte) (*Program, error)

// Disassemble returns canonical, re-assemblable bytecode text.
func (p *Program) Disassemble() string

// Assemble parses canonical assembly into a validated Program.
func Assemble(text string) (*Program, error)
```

`Compile` keeps its signature. `Exec`/`Execute` are unchanged; a loaded or
assembled program is an ordinary `Program`.

CLI:

```sh
# compile source to a binary image
tql --query-file migration.tql --emit-bytecode migration.tqlc

# run the image instead of the source
tql --program migration.tqlc config.yaml

# inspect or edit the text form
tql --program migration.tqlc --disassemble
tql --query-file migration.tql --disassemble
tql --assemble migration.tqla --emit-bytecode migration.tqlc
tql --assemble migration.tqla config.yaml
```

Flag rules:

- `--program FILE` is mutually exclusive with a query string, `--query-file` and
  `--assemble`. When it supplies the program, the first positional argument is
  the input document, not the query.
- `--assemble FILE` is mutually exclusive with a query source and follows the
  same positional rule.
- `--disassemble` prints the canonical text and exits; it accepts a query source,
  `--query-file`, `--program` or `--assemble` as its input.
- `--emit-bytecode FILE` compiles or assembles and writes the image, then exits;
  it requires a file path and does not accept `-` because stdout may carry results.
- `--emit-bytecode` and `--disassemble` are mutually exclusive.
- `--emit-bytecode` cannot be combined with `--in-place`, `--check` or
  `--values`; those describe document execution.
- Image writes are atomic and replace an existing image only after a complete write.

Errors for compile, load, assembly, disassembly and I/O use exit status 2, like
other query/input/write failures. Help text and `docs/cli.md` list the flags and
the `.tqlc` / `.tqla` naming convention.

## Compatibility and security

- A bytecode image is code, not data. Loading it is safe only in the sense of
  memory safety and bounded validation; executing it still obeys TQL's step limit
  and can perform expensive work.
- The loader checks every count against a fixed limit before allocating and
  checks every index before use. Integer overflow is treated as corruption.
- CRC32C detects accidental corruption. It is not an authentication mechanism.
- No `encoding/gob`, `unsafe`, reflection-based decoding or pointer values. The
  encoding is explicit and portable.
- Debug metadata contains parameter, slot and code names and statement indexes.
  It does not contain TQL source text or document contents.
- A format or opcode mismatch is an error. There is no silent fallback to the AST
  engine for a loaded image.

## Testing

| Layer | Cases |
| --- | --- |
| Golden images | One small program per feature; exact bytes; deterministic marshal; hex fixture reviewed |
| Corruption | Truncate every section, flip header/payload bytes, bad CRC, bad magic/version/flags, oversized counts, trailing bytes |
| Structure | Bad register/slot/constant/path/target/code/loop/scope index; bad jump; cyclic sub-codes; missing HALT; unknown opcode; malformed operator |
| Values | Null/bool/number spelling/string UTF-8/datetime/array/object order/duplicate keys/depth limit |
| Assembly | Numeric jump indexes, every directive, comments, typed constants, paths/targets, malformed lines with line numbers |
| Round-trip | Compile → marshal → load → disassemble → assemble → marshal equality; execute both sides; full test corpus |
| API/CLI | `MarshalBinary`, `LoadProgram`, `Disassemble`, `Assemble`; emit/run/disassemble/assemble flags; atomic writes; exit codes |
| Fuzz | Loader, assembler, disassembler round-trip; no panic; no unbounded allocation |
| Performance | Load time and image size compared with compile time; reported with the benchmark gate |

Fuzz targets run with the existing `make fuzz` family and CI. A discovered panic
or a non-deterministic image becomes a regression test.

## Migration steps

| Step | State on this branch |
| --- | --- |
| Stable opcode table, `Code`/`Program`, typed values, encoder/decoder, verifier, limits | Implemented; corruption, round-trip and loader fuzz tests pass |
| `Program` metadata: parameters and `Mutates` from code | Implemented; loaded and assembled programs are interchangeable |
| Disassembler, assembler, canonical directives and round-trip tests | Implemented; numeric jump indexes, JSON constants, full round-trip |
| Public API and CLI flags, docs, integration tests | Implemented; `--emit-bytecode`, `--program`, `--disassemble`, `--assemble` covered by CLI tests |

The AST reference engine remains only for differential tests. The performance
gate was met for expression-heavy work; mutation loops are neutral.

## Risks

| Risk | Mitigation |
| --- | --- |
| Format version drift | Stable IDs, explicit header version, golden images, reject mismatches |
| Loader panic or memory blow-up | Bounded reads, checked arithmetic, limits before allocation, fuzz |
| Assembly and disassembly diverge | One canonical grammar, round-trip corpus, deterministic directives |
| Loaded code bypasses `Mutates` | Derive mutability from opcodes, never from the file |
| Debug output becomes unstable | Comments are ignored; directives and numeric instruction indexes are semantic |
| Cross-platform byte differences | Fixed little-endian, fixed widths, sorted objects, no `int`/pointers |
| Users treat CRC as security | Document that validation, not checksum, is the safety boundary |

## Open questions

1. **Should `DEBUG` source ranges ship in v1?** Recommendation: no. Parameters,
   slots, code names and statement indexes are enough for the required
   disassembly; source ranges add size and a second compatibility surface.
2. **Should the image be compressed?** Recommendation: no in v1. Programs are
   small; a caller can compress the bytes without changing the format.
3. **Should `LoadProgram` accept older versions?** Recommendation: no. Recompile
   is simpler and safer than a migration path with no deployed fleet yet.
4. **Should assembly be a public format?** Recommendation: yes, as a debug and
   interchange format, with the same version discipline as the binary image.
5. **Should `Disassemble` print resolved values in operands instead of `K0`?**
   Recommendation: print IDs plus comments. Resolved-only output cannot round-trip
   without reparsing values and loses exact spellings.
