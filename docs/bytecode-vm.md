# Bytecode virtual machine

This is the entry point for the bytecode VM. The image and tooling are in
[bytecode format](bytecode-format.md); measurements and SIMD are in
[performance](bytecode-performance.md).

TQL parses a script once, lowers it to a register bytecode, and executes that
code against a document. The original source is still kept for targeted edits;
the VM never serializes a whole document.

## At a glance

| Question | Short answer |
| --- | --- |
| What is compiled? | The TQL script, not the JSON/TOML/YAML document |
| What is a register? | A stream of live nodes or a detached scalar |
| Where is state? | Per-execution slots, frames, scopes and loop references |
| How are edits applied? | The existing transactional edit engine, not the VM |
| Can code be saved? | Yes: `MarshalBinary` / `LoadProgram` |
| Can it be inspected? | Yes: `Disassemble` / `Assemble` and CLI flags |
| What runs in production? | The VM; the AST exists only while compiling |

## Execution model

```text
TQL source ── parser ── AST ── compiler ── immutable code
                                              │
document ── format parser ── tree ──────── VM + frames
                                              │
                                      selected edits
                                              │
                              existing edit/verify engine
                                              │
                                      commit or roll back
```

- `tql.Compile` parses and compiles. The returned `Program` holds parameter
  metadata, `Mutates`, and immutable code.
- `Document.Execute` validates parameters, runs the VM on a work copy, and
  commits the source and tree only after the whole script succeeds.
- An error leaves the original document untouched. `Result.Values` is discarded
  on any error.
- A compiled program holds immutable code; the parsed AST is discarded after
  compilation. `Mutates` is derived from the code, never trusted from a file.

## Compiler

`Compile` lowers `query.Program` to `bytecode.Program` in one pass.

- **Destination-passing.** `compileStream(e, dst)` and `compileScalar(e, dst)`
  write directly into a register, so branches share a result register.
- **Streams and scalars.** Most expressions produce streams. `ONE` turns a
  stream into exactly one detached scalar; `ARG` keeps zero/one live node for a
  built-in; `NODE` materializes a scalar when a stream is required.
- **Sub-codes.** Filters, dynamic selectors, `select` predicates and pipe right
  sides compile to small code objects. They share the script's slots but get
  their own register frame.
- **Fast paths.** Simple key/index filters become `MatchPlan` entries
  (`FILTER_MATCH` or a path `match` step). A right-hand path after `|` is folded
  into a `STEPS` plan. Complex predicates keep the generic filter path.
- **Arguments.** Built-in and tree calls use argument sets; malformed arity is a
  compile-time `FAIL` and does not evaluate arguments, without evaluating arguments.

### Instruction set

The numeric IDs are stable and append-only. The compiler, verifier, disassembler,
assembler and VM all read the same table.

| Group | Operations |
| --- | --- |
| Values | `CONST`, `CONST_VALUE`, `CTX`, `ROOT`, `VAR`, `MISSING_VAR`, `ONE`, `ARG`, `NODE`, `TRUTH`, `ANY_TRUTH`, `FAIL` |
| Paths | `STEPS`, `FILTER_STREAM`, `FILTER_MATCH` |
| Composition | `OBJECT`, `ARRAY`, `UNARY`, `BINARY`, `PIPE`, `CALL`, `SELECT`, `TREE`, `JUMP`, `JUMP_IF_FALSE`, `JUMP_IF_TRUE`, `JUMP_IF_NOT_NULL` |
| Statements | `READ`, `READ_VALUE`, `CHECK_DECLARE`, `DECLARE`, `ASSIGN`, `ASSIGN_CONST`, `DELETE`, `FOR_INIT`, `LOOP_ENTER`, `FOR_NEXT`, `FOR_CLOSE`, `LOOP_BREAK`, `LOOP_CONTINUE`, `ENTER_SCOPE`, `EXIT_SCOPE`, `STMT_ENTER`, `STMT_LEAVE`, `TICK`, `HALT` |

A `Code` object stores a name, register count and instructions. A `Program`
stores the constant pool, slot names, path plans, targets, match plans, scopes,
loops and code objects. Main code is index 0.

### Paths and matches

A path is data, not a chain of opcodes:

```text
key "services" → match name == "api" → key "replicas"
```

`STEPS` applies the plan to each input node: steps outside, nodes inside. It
preserves source order and error order. `FILTER_MATCH` handles a single
key/index lookup followed by truthiness or a literal comparison; anything more
complex uses a filter sub-code. `MatchPlan` is persisted with the program and
printed as `.match` in assembly.

`PIPE` evaluates its right side for each left value. A right-hand path is folded
into `STEPS`; a simple `select` is folded into `FILTER_MATCH` or
`FILTER_STREAM`.

## Value identity

The VM sometimes keeps live document nodes and sometimes detaches values. This
distinction is the most important rule for correctness.

| Expression | Result |
| --- | --- |
| `Literal` | Fresh node in stream context; plain scalar in scalar context |
| `Variable` | Live binding value; detached when consumed by `ONE` |
| `Path` | Live document/local nodes |
| `Object` values | Detached scalars |
| `Array` elements | Full streams; each value detached when the array is built |
| `Unary`, `Binary` | Scalar; `NODE` at a stream boundary |
| `and`, `or`, `??` | Scalar; short-circuit and nullness rules |
| Pipe operator | Live stream |
| Built-in input | `ARG`: live zero/one node; the helper detaches where needed |
| `select` predicate | Scalar via `ONE` |
| `walk`, `path`, `key` | Live streams |
| `read`, `for` source, `del` | Full streams |
| `filter` predicate | Scalar truth (`ANY_TRUTH`/match) or a full stream for complex predicates |

`ONE` is not a copy of a node: it stores `Interface()` and makes a fresh tree
only when `NODE` or `DECLARE` needs one. This avoids extra tree copies while
preserving `let` snapshots and `??` semantics.

## Variables and scopes

- Parameters occupy the first slots of the root scope. A `let` or loop variable
  gets a compile-time slot; shadowing is lexical.
- `CHECK_DECLARE` runs before the RHS, so a duplicate name errors without
  evaluating the value. An unreachable duplicate still does not error.
- `DECLARE` materializes the binding tree. Parameters are read-only.
- A name with no visible declaration compiles to `MISSING_VAR`, preserving the
  runtime `undefined variable` error in untaken branches.
- Scopes are slot ranges. Entering a scope clears its slots, which gives each
  loop iteration a fresh `let` environment.

## Control flow

`if` compiles a scalar condition and a conditional jump. Each body enters and
leaves a scope.

`while` emits a condition check and a body per iteration. `for` evaluates its
source once, builds a target queue, and uses `FOR_NEXT` to enter each iteration.
`break` and `continue` unwind scopes and statement frames to the loop boundary.

A `for` body without assignments or deletions is marked **read-only**:
`FOR_INIT` binds selected nodes directly instead of building address/reference
state. This is safe because a read-only body cannot invalidate the selection;
`path` and `key` still see live document nodes. Mutating loops keep references
so deletions and index shifts can be rebased.

## Assignments and transactions

Assignment does not reimplement the edit engine. The VM:

1. evaluates the target plan (`root`, current input, a base expression, or a
   general selector);
2. creates missing key/index steps only for `=` and `??=`;
3. groups targets by owner and removes duplicates/overlaps with `uniqueTargets`;
4. evaluates the RHS per target (`|=` uses the target as current input);
5. applies replacements as a batch, then pending creations;
6. lets the existing `replaceMany`/`createFrom`/`mutateOwned` code reparse,
   verify and rebase references.

The order matters: target selection, RHS evaluation, replacements, creations.
An error rolls back the whole execution. `Mutates` is true if the code contains
any `ASSIGN`, `ASSIGN_CONST` or `DELETE`, including unreachable code.

## Limits and errors

- 100,000 `TICK` events: one per statement, one per `while` condition, one per
  `for` iteration. This is the VM execution limit. The compiler emits a `TICK`
  in every loop, and validation rejects any code cycle without one, including
  assembled and loaded images.
- Block nesting 128, expression nesting 256, path length 256, enforced by the
  parser.
- Statement errors keep the `statement N:` prefix chain. Malformed loaded code
  is rejected by the loader; the VM also bounds-checks and never trusts indexes.
- `Program.Mutates` protects `Query` from programs that would edit documents.

## Persistence and tooling

```go
image, err := program.MarshalBinary()
loaded, err := tql.LoadProgram(image)
text := program.Disassemble()
assembled, err := tql.Assemble(text)
```

```sh
tql --query-file script.tql --emit-bytecode script.tqlc
tql --program script.tqlc config.yaml
tql --query-file script.tql --emit-assembly script.tqla
tql --program script.tqlc --disassemble
tql --assemble script.tqla config.yaml
```

See [bytecode format](bytecode-format.md) for the header, limits, validation,
assembly grammar and security rules.

## Testing and performance

- VM expression, path, statement and error tests cover reads, mutations and
  transactional rollback.
- Recipes, runtime ownership tests, golden images, loader corruption tests,
  assembly round-trips and fuzz targets cover the VM and format.
- `go test ./...`, `go test -race ./...`, `go test -tags purego ./...` and
  `GOEXPERIMENT=simd go test ./...` pass.

Read-heavy workloads were substantially faster than the former AST interpreter;
mutation loops and one-shot edits were neutral because parsing, editing,
reparsing and verification dominated them. Current numbers and reproduction commands are in
[performance](bytecode-performance.md#measured-results).

## Design notes

- **Register machine, not stack.** TQL expressions are streams; destination
  registers and per-code frames fit that model better than a value stack.
- **jq as prior art.** jq's single opcode table, constants pool, sub-codes,
  debug names and disassembler shaped this design. TQL adds versioned images,
  a loader/verifier and a re-assemblable text format; it does not copy jq's
  backtracking VM or 16-bit limits.
- **Tooling is separate.** `internal/bytecode` owns code, formats and tools;
  `tql` owns the VM and document mutations. This keeps the loader independent
  of `Document`.
- **No JIT.** Go, no third-party dependencies, and a document-oriented edit
  engine make a JIT a poor trade-off.
- **One execution engine.** The VM is the only interpreter. `internal/query`
  parses scripts and builds the AST consumed by the compiler; it does not execute
  programs.

## Where to change what

| Task | File |
| --- | --- |
| Opcodes, code/program types, constants | `internal/bytecode/code.go` |
| AST to bytecode, fast paths | `internal/bytecode/compile.go` |
| VM loop, frames, reports | `vm.go` |
| Paths, matches, assignments, deletes | `vm_target.go` |
| Binary image | `internal/bytecode/encode.go`, `decode.go` |
| Structural validation | `internal/bytecode/validate.go` |
| Disassembly and assembly | `internal/bytecode/disasm.go`, `asm.go` |
| Public API and CLI | `program.go`, `cmd/tql/main.go` |
| Behavior guarantee | `vm_internal_test.go`, `vm_expressions_test.go`, `benchmark_test.go` |
