# Bytecode format, persistence, and tooling

The VM itself is described in the [bytecode VM guide](bytecode-vm.md).

A compiled program can be saved as a versioned image, loaded without the TQL
source, printed as readable assembly, and assembled back. The text form is a
debugging and review format; the binary image is the deployment format.

## Code model

`internal/bytecode` owns the code representation. It depends on `syntax` and
`internal/query`, never on `tql`.

```go
type Program struct {
    Params  []Param
    Consts  []Const
    Slots   []string
    Paths   []PathPlan
    Matches []MatchPlan
    Targets []TargetPlan
    TargetSets [][]int32
    ArgSets    [][]int32
    Scopes     []Scope
    Loops      []Loop
    Codes      []*Code // index 0 is main
}

type Code struct {
    Name      string
    Registers int
    Ops       []Op
}
```

Opcode IDs are stable and append-only. A missing or unknown ID is an error, not
an alias for another instruction. Operand fields are fixed-width, so images do
not depend on the host word size. Every code object returns its result in `r0`;
main ends with `HALT`.

The extra pools are:

- `Slots` — lexical names, also used in dead-reference errors.
- `Paths` — key/index/wildcard/slice/filter/dynamic steps.
- `Matches` — one key/index lookup plus truthiness or a literal comparison.
- `Targets` — assignment and deletion targets.
- `Codes` — main plus predicate/selector/pipe sub-codes.

## Binary format

### Header

All integers are little-endian.

| Offset | Size | Field |
| ---: | ---: | --- |
| 0 | 4 | magic `TQLC` |
| 4 | 2 | format version, currently `1` |
| 6 | 2 | flags, reserved zero |
| 8 | 4 | header size, `24` |
| 12 | 4 | payload size |
| 16 | 4 | CRC32C (Castagnoli) of the payload |
| 20 | 4 | reserved, zero |
| 24 | ... | payload sections |

The checksum catches corruption; the loader and verifier are the security
boundary.

### Sections

```text
section := id u16 | flags u16 | length u32 | data[length]
```

| ID | Section | Contents |
| ---: | --- | --- |
| 1 | `META` | Opcode-table/format version |
| 2 | `PARAM` | Parameter names, required/default flags, typed defaults |
| 3 | `CONST` | Typed constant pool |
| 4 | `SLOT` | Slot names |
| 5 | `PATH` | Path plans |
| 6 | `TARGET` | Assignment/delete targets |
| 7 | `TARGETSET` | Code lists for `DELETE` |
| 8 | `ARGSET` | Register lists for calls/objects/arrays/tree functions |
| 9 | `SCOPE` | Lexical slot ranges |
| 10 | `LOOP` | Loop descriptors |
| 11 | `CODE` | Code objects |
| 12 | `MATCH` | Simple key/index/literal filter plans |

Sections are required in ascending order. Duplicate, unknown or out-of-order
sections are rejected. Sizes are bounded before allocation.

### Values

| Tag | Value | Payload |
| ---: | --- | --- |
| 0 | null | none |
| 1 | false | none |
| 2 | true | none |
| 3 | number | UTF-8 decimal lexeme, exact spelling retained |
| 4 | string | length + UTF-8 bytes |
| 5 | datetime | length + TOML date/time text |
| 6 | array | count + values |
| 7 | object | count + sorted key/value pairs |

Numbers never pass through `float64`. Object keys are sorted at encode time and
must be unique. `syntax.NormalizeValue` remains the authority for accepted
values.

### Determinism and versioning

`MarshalBinary` is deterministic for a given compiler version: fixed section
order, little-endian fields, sorted object keys, no pointers, no source paths,
no timestamps, no map iteration.

The format version is independent of the library SemVer. Any layout or opcode
semantic change bumps it. `LoadProgram` accepts only the version compiled into
that build:

```text
tql: bytecode format v2 is not supported by this build (supports v1); recompile the program
```

There is no implicit migration. Opcode IDs are never reused.

### Limits

| Limit | Initial value |
| --- | ---: |
| Image size | 64 MiB |
| Code objects | 4,096 |
| Instructions | 1,000,000 |
| Constants | 65,536 |
| Path/target/sets | 65,536 each |
| Slots and registers | 65,536 each |
| Path steps | 256 |
| Sub-code depth | 256 |
| Value nesting | 256 |
| String operand | 1 MiB |
| Path index / slice bound | signed 32-bit |

The compiler produces programs within these limits; the loader rejects images
outside them. Positive path indexes above the 32-bit range are rejected before
encoding; negative indexes and slice bounds clamp to the edge of the range
because values before the first element behave identically. The encoder and
decoder apply the same 1 MiB string limit.

## Loading and validation

`LoadProgram` is read-only: it does not execute code or touch a document.

1. Check magic, version, flags, header size, payload size and CRC32C.
2. Read sections with bounded lengths and checked arithmetic.
3. Decode typed values with UTF-8, number, datetime and depth checks.
4. Decode code, paths, matches, targets and cross-references.
5. Run structural validation.
6. Build an immutable `Program`.

The verifier checks at least:

- every register, slot, constant, path, match, target and code index is in range;
- every jump target is an instruction inside its own code;
- path/match steps have known kinds and valid references;
- loops and scopes reference valid instructions and slots;
- sub-code references form an acyclic graph within the depth limit;
- every control-flow cycle contains `TICK`, so the VM step limit applies to
  assembled and loaded code as well as to compiled scripts;
- operators and assignment operators are known;
- parameters are valid, unique, and have a default or required flag;
- code ends in `HALT` and the image has no trailing bytes.

Operand-kind mismatches at execution report `invalid bytecode: ...`; they never
panic. `Mutates` is derived from `ASSIGN`/`ASSIGN_CONST`/`DELETE`, never read
from the file, so a loaded program cannot bypass `Query`.

## Text assembly

The assembly is line-oriented and canonical. Lines beginning with `#` are
comments. Registers are numeric; constants are `K0`, paths `P0`, matches `M0`,
targets `T0`, sets `A0`/`D0`, codes `C0`, loops `L0`, scopes `S0`.

### Directives

| Directive | Meaning |
| --- | --- |
| `.version N` | Must match the build's format version |
| `.param "name" required` | Required input |
| `.param "name" default json <value>` | Input with a default |
| `.const KN json <json>` | Value constant; exact numbers survive |
| `.const KN text "..."` | Text constant |
| `.const KN texts ["..."]` | Object key set |
| `.slot SN "name"` | Slot name |
| `.path PN` | Start a path plan; following `.step` lines add steps |
| `.step key "k"`, `.step index N`, `.step wildcard`, `.step slice FROM TO`, `.step filter CN`, `.step dynamic CN`, `.step match MN` | One path step; `*` means an omitted slice bound |
| `.match MN key=K0 truth` | Truthiness match on a key |
| `.match MN key=K0 op=K1 value=K2` | Key compared with a literal |
| `.match MN index=N ...` | The same for an array index |
| `.target TN path root=BOOL base=N path=PN` | Path target; `base=-1` is current input |
| `.target TN expr CN` | General selector target |
| `.argset AN [registers]` | Register list for a call/object/array/tree function |
| `.targetset DN [CN ...]` | Code list for `DELETE` |
| `.scope SN [slots]` | Lexical slot range |
| `.loop LN KIND continue=N break=N scope=N varslot=N readonly=BOOL` | Loop descriptor; KIND is `for` or `while` |
| `.code CN "name" REGS` | Start a code object; `C0` is main |
| `.stmt N` | Statement index for following instructions |
| `.end` | Optional end of a path/code/file |

Jump operands are numeric instruction indexes. The VM validates every index.
Path index and slice operands are signed 32-bit; assembly rejects values outside
that range. Labels are a planned readability improvement, not a v1 requirement.

### Example

Actual output for `services[name == "api"].replicas = 2`:

```asm
# TQL bytecode v1
.version 1
.const K0 text "name"
.const K1 text "=="
.const K2 json "api"
.const K3 text "="
.const K4 json 2
.match M0 key=K0 op=K1 value=K2
.path P0
.step key "services"
.step match M0
.step key "replicas"
.target T0 path root=false base=-1 path=P0
.scope S0
.code C0 "main" 0
.stmt 1
STMT_ENTER 1
TICK
ASSIGN_CONST K3 T0 K4
STMT_LEAVE
HALT
```

The disassembler is deterministic. The central invariant is:

```text
MarshalBinary(Assemble(Disassemble(p))) == MarshalBinary(p)
```

`Disassemble(LoadProgram(MarshalBinary(p)))` produces the same canonical text as
`Disassemble(p)`.

## Public API and CLI

```go
func (p *Program) MarshalBinary() ([]byte, error)
func LoadProgram(data []byte) (*Program, error)
func (p *Program) Disassemble() string
func Assemble(text string) (*Program, error)
```

`Compile`, `Exec` and `Execute` keep their signatures. A loaded or assembled
program is an ordinary `Program`; parameters work through `WithParams`.

```sh
tql --query-file migration.tql --emit-bytecode migration.tqlc
tql --program migration.tqlc config.yaml
tql --program migration.tqlc --disassemble
tql --query-file migration.tql --disassemble
tql --assemble migration.tqla --emit-bytecode migration.tqlc
tql --query-file migration.tql --emit-assembly migration.tqla
tql --assemble migration.tqla config.yaml
```

- `--program` and `--assemble` supply the program; the first positional argument
  is then the input document.
- `--disassemble` prints canonical text and exits.
- `--emit-bytecode FILE` writes the image and exits; it requires a path and
  rejects `-`.
- `--emit-assembly FILE` writes canonical assembly and exits; use `-` for stdout.
- `--disassemble`, `--emit-assembly` and `--emit-bytecode` are mutually
  exclusive and cannot be combined with `--in-place`, `--check` or `--values`.
- Image writes are atomic.

## Security and compatibility

- A bytecode image is code. Loading validates memory safety and structure;
  executing it still obeys TQL's step limit and may be expensive.
- The loader checks counts before allocating and indexes before use.
- CRC32C is corruption detection, not authentication.
- No `encoding/gob`, `unsafe` decoding or pointer values in the format.
- Debug metadata contains parameter, slot and code names and statement indexes;
  it contains neither TQL source text nor document contents.
- A format/opcode mismatch is an error. There is no silent fallback for a
  loaded image.

## Testing

| Layer | Coverage |
| --- | --- |
| Images | Deterministic round-trip, golden bytes, empty/object/loop cases |
| Corruption | Truncation, bit flips, bad magic/version/CRC, oversized counts, trailing bytes |
| Structure | Bad indexes, jumps, match references, cyclic sub-codes, missing `HALT` |
| Values | Null/bool/number spelling/string/datetime/array/object/depth |
| Assembly | Every directive, match plans, numeric branches, malformed input |
| Round-trip | Compile → marshal → load → disassemble → assemble → marshal equality |
| CLI | Emit/run/disassemble/assemble, atomic writes, exit codes |
| Fuzz | `FuzzDecode`, `FuzzAssemble`, `FuzzProgramRoundTrip` |

The loader and assembler never execute input while validating it.
