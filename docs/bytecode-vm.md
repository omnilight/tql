# Bytecode virtual machine

Status: implementation plan. This document describes a design; the branch contains
no VM code yet.

Related plans:

- [Bytecode format, persistence, and assembly](bytecode-format.md) — binary
  save/load, validation, disassembly, assembler, and jq prior art in detail.
- [Performance and SIMD](bytecode-performance.md) — the AST-versus-VM proof,
  benchmark gates, and where SIMD is worth using.

## Contents

- [Feature map](#feature-map)
- [Goal](#goal)
- [Current engine](#current-engine)
- [Prior art: jq](#prior-art-jq)
- [Architecture](#architecture)
- [Bytecode format](#bytecode-format)
- [Compilation](#compilation)
- [Execution](#execution)
- [Testing and parity](#testing-and-parity)
- [Migration steps](#migration-steps)
- [Risks](#risks)
- [Open questions and recommendations](#open-questions-and-recommendations)

## Feature map

| Requirement | Where it is planned |
| --- | --- |
| Look at jq's bytecode design | [Prior art](#prior-art-jq) and [jq lessons](bytecode-format.md#lessons-from-jq) |
| Save and load bytecode | [Binary format](bytecode-format.md#binary-format) and [loader](bytecode-format.md#loading-and-validation) |
| Human-readable disassembly | [Text assembly](bytecode-format.md#text-assembly) and `Program.Disassemble` |
| Assemble text back into a program | [Text assembly](bytecode-format.md#text-assembly) and `Assemble` |
| Prove speed and efficiency against AST | [Performance and SIMD](bytecode-performance.md#gate) |
| Use SIMD where it helps | [SIMD applicability](bytecode-performance.md#simd-applicability) |

## Goal

Execute compiled TQL scripts in a bytecode virtual machine instead of walking the
AST recursively. Compile once in `tql.Compile`, then run the same immutable program
against any number of documents.

The bytecode is also a first-class artifact:

- save it to a versioned binary image and load it later;
- disassemble it into readable, re-assemblable text for debugging;
- assemble that text back into a program;
- prove that the VM is not slower than the AST engine before it becomes the
  default;
- use SIMD only where it actually pays for itself.

The VM must be an internal replacement, not a language change:

- identical results, source bytes, `Changed` flags, and error text;
- identical transaction, scope, reference, limit, and parameter behavior;
- no new third-party dependencies and no Go version bump.

The reason to do it is execution cost and headroom: today every expression
evaluation dispatches through the `query.Expr` interface, rebuilds the visible
variable map for every expression, and allocates a scope map per block. A flat
instruction stream removes that dispatch, resolves variables to frame slots at
compile time, and gives control flow an explicit shape that later optimizations
(constant folding, fused path steps, fast assignment paths) can target without
touching the parser.

This is not a performance guarantee. Parsing, format editing, reparsing, and
semantic verification dominate most edit workloads, and the VM does not change
those costs. Gains are expected for expression-heavy reads, filters, `walk`, and
loop-heavy scripts; edit-dominated workloads should stay within noise.

## Non-goals

- No new syntax, built-ins, coercion rules, or error messages.
- No streaming execution. Memory still grows with source, tree, selected targets,
  and result values, as documented in [performance](performance.md).
- No change to `syntax.Format`, the editing contract, or `Document.Apply`.
- No JIT or native code generation.
- No forward or backward compatibility guarantee across bytecode format
  versions. The format is versioned, and a mismatch is rejected with an
  actionable error instead of being guessed.
- No source-level stepping debugger in the first version. Disassembly, canonical
  assembly, and a later execution trace are enough for diagnostics.
- No custom SIMD in the dispatch loop or stream copies without a measured win.
  The existing scanner SIMD stays in use; see
  [performance and SIMD](bytecode-performance.md#simd-applicability).

## Current engine

The pieces a VM has to replace or reuse:

| Piece | Location | Role |
| --- | --- | --- |
| `tql.Compile` | `program.go` | Parses TQL into `query.Program`; currently no code generation |
| `Document.execute` | `document.go` | Validates parameters, copies the document, runs statements, commits |
| `scriptRuntime` | `runtime.go` | Recursive statement execution, scopes, loop references, step limit |
| `query.Expr.Eval` | `internal/query/eval.go` | Expression evaluation returning a stream of nodes |
| Assignment planning | `execution.go`, `mutation.go`, `address.go` | Target selection, creation, batched edits, rebasing |
| Reference rebasing | `runtime.go` (`rebase`) | Keeps loop references valid after structural edits |
| Limits | `runtime.go`, `parse.go` | 100,000 steps, 128 block levels, 256 expression levels |

Expression evaluation is stream-based: every expression yields `[]*syntax.Node`,
and the current input (`.`) is passed separately from the document root (`$`).
Some operations keep those nodes live, while others deliberately detach them into
new value trees. The VM must preserve that distinction exactly; see
[value identity](#value-identity-and-detach-points).

## Prior art: jq

jq is the closest existing design, so the VM borrows its proven shape without
copying its semantics. The notes below refer to jq master checked on 2026-10-27:
[`opcode_list.h`](https://github.com/jqlang/jq/blob/master/src/opcode_list.h),
[`bytecode.h`](https://github.com/jqlang/jq/blob/master/src/bytecode.h),
[`bytecode.c`](https://github.com/jqlang/jq/blob/master/src/bytecode.c),
[`compile.c`](https://github.com/jqlang/jq/blob/master/src/compile.c), and
[`execute.c`](https://github.com/jqlang/jq/blob/master/src/execute.c).

- jq parses a filter into a linked `struct inst` IR, binds variables and
  functions, then calls `block_compile` to produce an in-memory `struct bytecode`.
  `jq_next` executes it as a stack machine.
- Branches, constants, locals, closures, subfunctions and C built-ins are
  separate pools in `struct bytecode`. `debuginfo` keeps function names, parameter
  names and local names for diagnostics.
- `src/opcode_list.h` is the single source of truth for opcode number, mnemonic,
  operand flags, instruction length and stack effects. The compiler, VM and
  disassembler all include it.
- `dump_disassembly` and `dump_operation` print program counters, mnemonics,
  branch targets, constant previews, variable names with frame levels, built-in
  names and subfunctions. The CLI exposes this with `--debug-dump-disasm`;
  `--debug-trace` prints every executed instruction and its stack values.
- After compilation, jq runs a peephole pass (currently tail-call detection).
  Its compiler emits forward branches only and uses 16-bit code and operand
  fields, with explicit size errors.
- jq has **no** bytecode serializer, loader or assembler. `struct bytecode` holds
  `jv` values and C function pointers, so it is in-memory only and cannot be
  written to a portable file. Disassembly is a debug print, not an input format.

What TQL adopts: a compact instruction stream, a single opcode table shared by
compiler/VM/verifier/disassembler/assembler, a constants pool, named slots and
sub-codes in debug output, a post-compile optimization pass, and jq-style
instruction-level diagnostics. What TQL does not copy: fork/backtracking
(execution here uses materialized stream operands), closures and user functions
(TQL scripts have neither), 16-bit limits, or an in-memory-only program.

What TQL adds beyond jq: a versioned binary image, a defensive loader, canonical
re-assemblable assembly, and a performance gate against the AST engine. The
format and tooling details are in
[bytecode format and tooling](bytecode-format.md).

## Architecture

```text
TQL text ── lexer/parser ── AST (internal/query)
                               │
                  compiler (internal/bytecode)
                               │
                     immutable code (internal/bytecode)
                     ┌─────────┴──────────┐
              binary image            assembly text
                     │                     │
               LoadProgram               Assemble
                     └─────────┬───────────┘
                               │
document bytes ── parser ── tree
                               │
                       VM + frames (package tql)
                               │
                     statement effects
                               │
                     existing edit engine
                               │
                     commit or roll back
```

The split keeps tooling independent of `Document`:

- `internal/bytecode` — stable opcode table and IDs, code representation,
  typed constants, path plans, AST compiler, binary encode/decode, structural
  verifier, disassembler and assembler. It imports `internal/query` and
  `syntax` for value and step types, but never `tql`.
- `tql` — VM and statements (`vm.go`), `Program`, bindings, references,
  rebasing, step accounting, and the public compile/load/assemble wrappers.
- `cmd/tql` — flags for emitting, loading, disassembling and assembling programs.

Statements mutate `Document` and use package-private ownership rules, so the VM
stays in `tql` instead of inventing a host interface before there is a second
host. `internal/query` remains the parser and reference semantics layer. Shared
value rules are extracted as pure helpers (see
[shared semantics](#shared-semantics-with-the-query-package)) so the VM does not
reimplement operators or built-ins. If a second execution host ever appears, the
VM can move to `internal/vm` behind that host boundary; doing it now would be
speculative.

Planned files:

```text
internal/bytecode/
  opcodes.go     stable IDs, mnemonics, operand layouts
  code.go        code, constants, path plans, loops, scopes
  value.go       typed constant encoding
  compile.go     AST -> bytecode
  encode.go      deterministic binary writer
  decode.go      bounded binary reader and loader
  verify.go      structural validation
  disasm.go      canonical, annotated assembly output
  asm.go         assembly parser and label resolution
tql/
  vm.go          frames, expressions, statements, loops, errors
  program.go     params, mutates, code; marshal/load/disassemble/assemble API
```

`Compile` keeps returning only parser errors plus, theoretically, an internal
code-generation error. Every AST node the parser can produce must have a compiler
case; exhaustive tests make a missing case a build failure, not a user-facing
error. `Program` keeps parameter metadata, the derived `Mutates` flag and the
code; the AST is discarded after compilation. `Mutates` is computed with the
existing `query.Program.Mutates` rule at compile time and derived from the code
for loaded or assembled programs; the bytecode does not redefine the rule.

## Bytecode format

The machine is register-based. A register holds an operand: either a **stream**
of nodes or a **scalar** produced by `ONE` or a scalar-valued operator.

```go
type operand struct {
    nodes  []*syntax.Node // stream
    value  any            // scalar
    scalar bool
}

type Code struct {
    Name      string // debug name: main, predicate, selector, ...
    Registers int    // maximum value registers in one frame
    Ops       []Op
    Loops     []LoopInfo
    Scopes    []ScopeInfo
}
```

The program-level pools (`Consts`, `Slots`, `Paths`, `Targets`, `Codes`) live next
to the code objects and are immutable after compilation. The compiler is the only
writer; the VM only reads.

```go
// Conceptual in-memory layout; the persisted layout is fixed in the format doc.
type Op struct {
    ID   uint8 // stable opcode number
    A, B, C, D int32 // register, constant, plan, sub-code, or jump operands
    Stmt int32 // statement index for error prefixes
}
```

A register machine fits the existing stream model better than a stack machine:
`Path.Eval` already maps a stream through steps, `|` evaluates a right side per
left value, and filters evaluate a predicate per item. Destination-passing
compilation (`compileExpr(expr, dst)`) avoids stack shuffling and keeps branch
results in a stable register.

Sub-codes are used for predicates, dynamic selectors, `select`, and pipe right
sides. They share the script's variable slots but have their own register frame
and current input. This keeps nested evaluation explicit without a general
function-call mechanism.

### Stable opcode table

Opcode numbers are part of the bytecode format. One table owns each opcode's
numeric ID, mnemonic, operand layout, and expected operand kinds:

```go
type opInfo struct {
    id      uint8
    name    string
    operands operandLayout
}
```

The compiler, VM, verifier, disassembler and assembler all read this table.
Numeric IDs are append-only: an ID is never reused or renumbered, and an unknown
ID is a load error. This is the same discipline jq gets from
`opcode_list.h`, extended to a persisted format. The full format rules are in
[bytecode format and tooling](bytecode-format.md).

### Instruction set (v1)

Value and variable ops:

| Op | Semantics |
| --- | --- |
| `CONST dst, k` | One-node stream from a literal, materialized fresh |
| `CONST_VALUE dst, k` | Scalar literal, without building a node |
| `CTX dst` | Current input stream; a fresh null node if the current reference is dead |
| `ROOT dst` | Document root stream |
| `VAR dst, slot` | Binding value stream; live for references, snapshot for `let` |
| `MISSING_VAR name` | Runtime `undefined variable $name` error; used where the compiler cannot resolve a lexical slot |
| `ONE dst, src` | Exactly one value from a stream, detached to a scalar via `Interface()`; empty is a nil scalar; more than one is an error |
| `NODE dst, src` | Materialize a scalar as a fresh one-node stream; a nil scalar becomes null |
| `ARG dst, src` | Zero or one value for a built-in as a stream, keeping node identity; empty is a null node; more than one is an error |

Path and composition ops:

| Op | Semantics |
| --- | --- |
| `STEPS dst, src, plan` | Apply a compiled step plan to every node in `src` |
| `OBJECT dst, keys, args` | Build an object; each arg is a scalar register |
| `ARRAY dst, args` | Build an array; each arg is a full stream, flattened |
| `UNARY dst, src, op` | Unary operator on a scalar; result is scalar |
| `BINARY dst, l, r, op` | Non-short-circuit binary operator on scalars; result is scalar |
| `JUMP target` | Unconditional jump |
| `JUMP_IF_FALSE src, target` | Read truth from a scalar |
| `JUMP_IF_TRUE src, target` | Read truth from a scalar |
| `JUMP_IF_NOT_NULL src, target` | Read nullness from a scalar; used by `??` |
| `PIPE dst, src, code` | Evaluate `code` for each node in `src`, with that node as current input; concatenate |
| `CALL dst, name, args` | Built-in call; args are `ARG` registers |
| `SELECT dst, code` | Evaluate predicate code (ending in a scalar); return the current input stream if truthy |
| `TREE dst, name, args` | `walk`, `path`, or `key`; args are full streams |
| `TICK` | Count one interpreter step for limit parity |

Statement and control ops:

| Op | Semantics |
| --- | --- |
| `READ src` | Append `Interface()` of every node to `Result.Values` |
| `CHECK_DECLARE name` | Fail if the current scope already declares `name`; does not activate it |
| `DECLARE slot, src` | Activate `slot` from a scalar, materializing its tree |
| `ASSIGN op, target, rhs` | Assignment or compound assignment, using a compiled target plan and a scalar RHS |
| `DELETE targets` | Evaluate target streams and delete their nodes |
| `FOR_INIT iter, src` | Evaluate the loop stream once and build the reference queue |
| `FOR_NEXT iter, done` | Leave the previous iteration scope, enter the next live iteration, or jump to `done` |
| `FOR_CLOSE iter` | Restore current input, truncate references, leave the loop scope |
| `LOOP_BREAK loop`, `LOOP_CONTINUE loop` | Unwind to the loop's saved scope, then jump; `FOR_CLOSE` truncates references |
| `ENTER_SCOPE id`, `EXIT_SCOPE id` | Activate or leave a lexical scope |
| `HALT` | End the program |

`ASSIGN` encodes `=`, `|=`, `+=`, `-=`, `*=`, `/=`, and `??=`. `|=` evaluates
the RHS with the target node as current input; the other operators evaluate it
with the statement's current input. `=` and `??=` may create missing path steps;
the other operators and arbitrary selector targets may not. This mirrors
`Document.statement` exactly.

Truth follows `query.Truth`: only `false` and `null` are false, while `0` and the
empty string are true. `JUMP_IF_NOT_NULL` checks scalar nilness, not truth. Go
zero values must never be used as falsy shortcuts.

### Path plans

A path is data, not a sequence of opcodes:

```go
type pathPlan struct {
    steps []stepPlan
}

type stepPlan struct {
    step      query.Step // key, index, wildcard, slice, filter, dynamic
    selector  *code      // Kind == dynamic
    predicate *code      // Kind == filter
}
```

`STEPS` walks steps and nodes in the same order as `query.Path.Eval`: steps
outside, nodes inside. This keeps the first error identical when different nodes
would fail at different steps. Simple steps call `query.Step.Eval` directly,
which keeps the exact key/index/wildcard/slice rules in one place. Filter and
dynamic steps use their compiled sub-codes. Assignment uses the same plan for
`Resolve` and `Eval`, so creation, target selection, and reads cannot drift.

`query.Step.Eval` on a dynamic step currently resolves the selector first; the VM
does the same through a new `query.SelectorStep(value any) (query.Step, error)`
helper extracted from `Step.Resolve`. The parse-time distinction between a filter
predicate and a dynamic selector stays in the parser; the compiler must respect
`Step.Kind` and never re-classify an expression.

## Compilation

### Expressions

Compilation is destination-passing: `compileExpr(e, dst)` writes e's result into
`dst`, allocating temporaries from a small free list. The compiler has two modes
that follow the reference evaluator's boundaries:

- `compileStream(e, dst)` guarantees a stream. It is used by `read`, `for`
  sources, `del` targets, path bases, `PIPE` left sides, `ARRAY` elements, `walk`/
  `path`/`key` arguments, and filter predicates.
- `compileScalar(e, dst)` guarantees a scalar. It is used wherever the reference
  evaluator calls `One`: `let`, conditions, assignment RHS, object values,
  `select` predicates, and binary or unary operands.

A naturally scalar expression consumed as a stream gets a `NODE`; a literal in
scalar context uses `CONST_VALUE` and never builds a node. This keeps the branch
joins simple and avoids extra tree copies for assignments and object values.
Operand lists for `OBJECT`, `ARRAY`, `CALL`, and `TREE` occupy a contiguous
register block that the compiler allocates and releases as a unit.

Sub-codes are emitted for:

- filter predicates (`[pred]`), which return a full stream and are true if any
  result is truthy;
- dynamic selectors (`[expr]`), where the sub-code returns the full stream and
  `SelectorStep` requires exactly one string or integer;
- `select(...)`, whose predicate ends with a scalar;
- the right side of `|`.

`select` with exactly one argument becomes `SELECT`; any other arity becomes
`CALL`, where the shared helper returns the existing arity error. Tree functions
keep their arity checks in `ApplyTreeFunction`.

The compiler tracks the current statement index and emits statement bodies in
order. Labels are patched after emission; there is no symbolic linker.

### Value identity and detach points

This is the most important compatibility rule. The current evaluator keeps live
document nodes in some places and detaches them into fresh trees in others. The
compiler must follow this table exactly:

| AST construct | Conversion | Identity |
| --- | --- | --- |
| `Literal` | `CONST` / `CONST_VALUE` | Fresh node in stream context; plain scalar otherwise |
| `Variable` | `VAR` (+ `ONE` when scalar) | Live stream; detached scalar when consumed by `ONE` |
| `Path` | `STEPS` (+ `ONE` when scalar) | Live document/local stream; detached scalar when consumed |
| `Object` values | `compileScalar` | Detached scalar |
| `Array` elements | Full streams | Each value detached when the array is built |
| `Unary` operand | Scalar | Scalar result; `NODE` at a stream boundary |
| `Binary` operands | Scalars | Scalar result; `NODE` at a stream boundary |
| `and`, `or` | Scalars + jumps | Boolean scalar |
| `??` | Scalars + `JUMP_IF_NOT_NULL` | Detached scalar; nullness, not truth, selects the right side |
| Pipe operator | `PIPE` | Live stream |
| Built-in input | `ARG` streams | Live nodes; the shared helper detaches where it already does |
| `select` predicate | Scalar | Detached |
| `walk`, `path`, `key` args | Full streams | Live |
| `let`, `if`, `while`, assignment RHS | `compileScalar` | Detached scalar; `DECLARE` materializes the binding tree |
| `read`, `for` source, `del` targets | Full streams | Live until explicitly detached |
| `filter` predicate | Full stream | Live; truth from `Interface()` |

Examples of why this matters: `let $copy = items` must produce a snapshot, and
`$x ?? fallback` must not turn a detached left side into a live subtree. A VM
that passes nodes through every op would change loop ownership and local mutation
behavior even when the printed value looks the same.

`ONE` stores the detached scalar directly; `NODE` materializes it only when a
stream consumer needs a node, and `DECLARE` materializes it for the binding tree.
`ARG` is the built-in-specific variant that keeps the node when exactly one value
is produced, because `length()` and `type()` inspect tree metadata without
detaching. This follows `query.One` and `query.Node` exactly while avoiding an
extra tree copy for assignments and object values.

### Variables and scopes

Parameters occupy the first slots of the root scope. Every `let` and every loop
variable gets a compile-time slot; shadowing is lexical, so `VAR` needs no name
lookup at runtime.

A reference to a name with no declaration visible at that point compiles to
`MISSING_VAR`. It must not become a compile-time error: a reference in an untaken
branch, such as `if false { $unknown }`, is valid today and fails only if reached.
The same applies to a declaration made inside an untaken block and referenced
outside it. `MISSING_VAR` keeps the existing error text and timing.

Runtime scopes are slot ranges. A `let` compiles to `CHECK_DECLARE`, the RHS
code, then `DECLARE`. This preserves the current order: the duplicate name is
checked **before** the RHS is evaluated, and the new name is not visible to its
own RHS. An unreachable duplicate `let` is still not an error. A parameter name
redeclared by a top-level `let` is caught by the same check because parameters
are in the root scope. Inner scopes may shadow parameters, as today.

Slots hold `*binding`, so references remain stable even if the slot slice grows:

```go
type binding struct {
    value    *syntax.Node // snapshot for let, parameter, or computed loop value
    ref      *nodeRef     // live reference for a document/local loop item
    readonly string       // parameter name
}
```

`VAR` returns the binding's live value (or the located node for a reference).
A dead reference is not a null value: `VAR` reports `variable $name has no value`,
so slot metadata keeps the name for that message. `owner()` scans active scopes
from innermost to outermost for a non-reference binding whose value tree is the
target's top node. That preserves the current ownership rule for document edits
versus local snapshots.

### Control flow

`if` compiles its condition to a scalar, then `JUMP_IF_FALSE`, two bodies, and a
join. Each body gets its own `ENTER_SCOPE`/`EXIT_SCOPE`.

`while` compiles to a scalar condition head and a body with its own scope. A
`TICK` is emitted at the statement entry and at every condition check, matching
`block` plus the current `while` loop.

A `for` body shares the iteration scope with the loop variable: the compiler must
not open a second scope around the body, because a body `let` is visible to the
rest of that iteration and disappears on the next one.

`for` has an iterator record in a slot:

1. `FOR_INIT` evaluates the source once and builds a queue. Each selected node is
   resolved to an owner and address and gets a `nodeRef`; nodes that do not
   belong to the document or a local binding become independent snapshots. This
   is exactly the current `iterate` setup, including its `addr` error path.
2. `FOR_NEXT` skips dead references, sets the loop variable slot, enters the loop
   scope, and makes the binding the current input.
3. The body runs. `LOOP_CONTINUE` jumps to `FOR_NEXT`; `LOOP_BREAK` jumps to the
   close path. `LOOP_BREAK` also serves `while`, where the targets are the
   condition head and the code after the body.
4. `FOR_CLOSE` restores the previous current input, truncates references to the
   loop's starting length, and leaves the loop scope.

A `TICK` is emitted before every `FOR_NEXT`, including iterations whose reference
has died, because the current loop counts those too. Nested loops hold
independent iterator slots.

### Assignments and deletion

`ASSIGN` reuses the current algorithm rather than inventing a second one:

- a plain path target (`Path` with `Root`, a base expression, or current input)
  supports creation for `=` and `??=`; a base expression may produce several
  bases, and each is walked independently;
- any other selector target is evaluated as a stream and grouped by owner;
- each group is deduplicated and sorted with `uniqueTargets`;
- each target's current value is compared with the scalar RHS using `Equal`
  before an update is queued;
- all independent replacements are applied through `replaceMany`;
- pending creations are applied after replacements, using `createFrom`;
- every applied operation triggers the existing `rebase` call.

The order is visible in errors and must be preserved: select and group targets
first, then evaluate each target's RHS in target order (`|=` uses the target as
current input), then evaluate a pending creation value once if creation is
needed, then apply replacements, then apply creations.

The target walk in `assignmentPath` moves to a method on `pathPlan` that accepts
callbacks for "reached target" and "needs creation". `Document` keeps ownership,
writability, address calculation, edit application, verification, and rebasing.
The same plan powers read paths, so a dynamic selector resolves identically on
both sides.

`DELETE` evaluates all target streams, groups them, and performs the same
sequential `mutateOwned("delete", ...)` calls, so reference rebasing and
"deletion target disappeared" errors stay identical.

### Limits and errors

The VM counts the same logical events as the interpreter, not raw instructions:

- one `TICK` per statement execution;
- one per `while` condition check;
- one per `for` iteration, before the dead-reference check.

This keeps the 100,000-step limit meaningful: a program that finishes today must
finish, and an empty infinite loop must fail. The message remains
`script execution limit exceeded (100000 steps)`.

The parser still enforces block depth 128, expression depth 256, operator count
256, and path length 256. Compiler recursion follows those bounds.

Statement errors preserve the current prefix chain: `statement 2: statement 1:
...`. The VM keeps a stack of active statement indexes and formats the same text
when an error unwinds. Golden tests must cover top-level statements, nested
blocks, loop bodies, condition errors, tick errors, and errors from assignment
planning.

## Shared semantics with the query package

The VM must not fork language rules. Extract the pure parts of the existing
evaluator into helpers that both implementations call:

- `query.ApplyBuiltin(name string, ctx *syntax.Node, args []*syntax.Node)` —
  the bodies of `length`, `keys`, `values`, `type`, `sort`, `reverse`, `first`,
  `last`, `has`, `contains`, `startswith`, `endswith`, and `test`, plus the
  `select` arity error, including current argument-count and error behavior;
- `query.ApplyTreeFunction(name string, ctx, root *syntax.Node, args []*syntax.Node)`
  — `walk`, `path`, and `key`, including postorder and parent-path rules;
- `query.UnaryValue(op string, v any) (any, error)`;
- `query.SelectorStep(v any) (query.Step, error)` — extracted from `Step.Resolve`;
- existing `query.Operation`, `query.Equal`, `query.Truth`, `query.Node`, and
  `query.Step.Eval`.

`Call.Eval`, `Unary.Eval`, and `Step.Resolve` become thin wrappers over these
helpers. This is a behavior-preserving refactor that can land before the VM and
is covered by the existing tests.

## Execution

```go
type vm struct {
    doc     *Document
    result  *Result
    slots   []*binding
    scopes  []slotRange
    frames  []frame // reused by nesting depth
    refs    []*nodeRef
    current *binding
    steps   int
    stmts   []int // active statement indexes for error prefixes
    loops   []loopState
}
```

A frame holds a code pointer, registers, current input, and program counter.
Sub-code evaluation uses the same variable slots and a frame from the depth
pool; filters and pipes reuse the frame across items instead of allocating per
item. Registers are reset by length, not reallocated, once a frame has grown.

`Document.execute` keeps its current shape:

1. validate parameters and build independent read-only bindings in declaration
   order, so each parameter lands in its compiled slot;
2. copy the document into a work `Document`;
3. run the VM on the work document;
4. on success, clone bytes if requested, compute `Changed`, and commit;
5. on any error, return it and leave the original document untouched.

`Document.runtime` points at the VM state so `mutateOwned`, `mutate`, and
`replaceBatch` can keep calling `rebase`. Local bindings still hold their own
trees, and `mutateOwned` still replaces a local binding's whole tree, so the
transaction boundary does not change.

Compiled code and constants are immutable after `Compile`; a `Program` remains
safe for concurrent reuse. Constants store plain values and are materialized per
evaluation, so no execution can expose a shared mutable tree. Frames, slots, and
references are per execution.

`Program` keeps the parameter metadata, the derived `Mutates` flag and the
compiled code. The AST is not needed after compilation, so a program loaded from
a binary image or assembled from text behaves exactly like a freshly compiled
one. `Mutates` is derived from the code, not trusted from the file, so a loaded
program cannot smuggle a mutation past `Query`.

## Value identity, aliases, and rollback

The VM does not introduce caching of live nodes across statements. Every
statement re-evaluates its targets against the current work document, as today.
`replaceMany` still batches independent document replacements against one source
snapshot and falls back to sequential edits for overlapping YAML alias envelopes.
Every applied document edit still reparses and verifies the full value tree; local
binding updates still replace the binding's tree without parsing source.

Deleting an array element still rebases references for later iterations and
invalidates descendants; replacing a parent still cancels future visits to its
former descendants. The existing tests in `runtime_test.go` are the acceptance
suite for these rules, and the VM runs the same algorithms.

## Testing and parity

The migration uses three layers of evidence:

1. **Compiler tests.** Golden disassembly for small programs covering every
   opcode, branch shape, filter, dynamic selector, pipe, `for`, `break`/
   `continue`, and assignment form. A test asserts that every AST node type in
   the parser's grammar has a compiler case.
2. **Differential tests.** During migration, run the AST engine and the VM on
   separate copies of the same document and compare `Result.Values`, `Result.Bytes`,
   `Result.Changed`, and the full error string. Sources: `runtime_test.go`,
   `internal/query/script_test.go`, `document_test.go`, `examples/`, recipes,
   and the seeds in `FuzzExec`.
3. **Golden corpus after the switch.** Freeze a corpus of
   `source + program + expected values/bytes/error` captured from the AST engine
   before it is removed. The VM must keep passing it without the old engine.

Additional focused tests:

| Area | Cases |
| --- | --- |
| Streams | Empty stream, one value, many values, `ONE` versus `ARG`, null versus empty |
| Paths | Key, index, negative index, wildcard, slice, filter, dynamic key/index, nested filters |
| Built-ins | Every function, arity errors, zero/one/two-argument forms, `walk` postorder, `path`/`key` after deletion |
| Scopes | Shadowing, redeclaration timing, parameters read-only, loop variable per iteration, nested blocks |
| References | Deleted siblings, index shifts, replaced parents, local trees, computed values, nested loops |
| Assignments | Creation, `??=`, pipe-assignment current input, slices, overlapping targets, local owners, YAML aliases, batch fallback |
| Limits and errors | Step limit, exact prefixes, condition and tick errors, rollback after every failure |
| API | `Compile` reuse, `Mutates`, `WithParams`, concurrent `Execute`, `Query` rejection |
| Fuzz | Existing `FuzzExec` plus a temporary AST-versus-VM parity fuzz target |

Loader, assembler, corruption and round-trip tests are specified in
[bytecode format and tooling](bytecode-format.md#testing). The A/B benchmarks and
SIMD gates are specified in [performance and SIMD](bytecode-performance.md).

Run the normal gates from [CONTRIBUTING](../CONTRIBUTING.md):

```sh
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
make fuzz FUZZTIME=10s
```

## Performance evidence

The production switch is gated on a predeclared A/B measurement, not intuition.
Correctness parity comes first; then the same documents run through the AST
engine and the VM on the same machine. The gate covers:

- expression-heavy reads, filters, `walk`, and loop scripts, where the VM must
  show a statistically significant win;
- edit-dominated workloads, where the VM must stay within noise;
- allocations per operation, not only wall time;
- one-shot `Exec`/`Eval`, where code generation is paid on every call;
- compiled-program reuse and load time, which should beat recompiling source.

The exact workloads, commands, thresholds, reporting format, and SIMD suitability
matrix are in [performance and SIMD](bytecode-performance.md). If the gate is not
met, the VM stays an internal experiment and the AST engine remains the default.
That is a valid outcome, not a failure to hide.

## Migration steps

Each step is reviewable on its own and leaves the repository green. Until step 7,
production still executes the AST; code generation stays behind an internal test
hook so one-shot `Exec`/`Eval` cost and behavior do not change before the switch.

| Step | Work | Exit criterion |
| --- | --- | --- |
| 0 | Extract shared value helpers in `internal/query`; add the parity harness, A/B benchmark skeleton, and baseline | Existing tests and benchmarks unchanged |
| 1 | Add `internal/bytecode` code types and expression compiler; add the expression VM in `tql`; compile `Path`, literals, objects/arrays, operators, built-ins, filters, dynamic selectors, pipes, and tree functions | Differential expression tests pass on all read-only fixtures; production still uses the AST |
| 2 | Add statement code generation for `let`, `if`, `while`, `for`, `break`, `continue`, `read`; add slots, scopes, iterators, refs, ticks, and error prefixes | Read-only scripts (including `for` with `path`/`key`) match the AST engine |
| 3 | Add compiled assignment and deletion targets; refactor `assignmentPath` onto `pathPlan`; reuse `replaceMany`, `createFrom`, `mutateOwned`, and rebasing | Full script parity, including recipes, rollback, aliases, local trees, and parameter errors |
| 4 | Add the stable opcode table, versioned binary encoder/decoder and structural verifier; persist parameters, slots, paths, loops, scopes and sub-codes | Golden images, corruption matrix and loader fuzz pass; malformed input never panics |
| 5 | Add canonical disassembly and the assembler with labels, typed constants, path/sub-code sections and debug comments | Compile → disassemble → assemble → marshal round-trips exactly; assembled programs pass parity |
| 6 | Run the full AST-versus-VM benchmark gate; audit SIMD opportunities and prototype only candidates that pass the measurement rule | The gate in [performance and SIMD](bytecode-performance.md) is met and reported |
| 7 | Switch `Document.execute` to the VM; keep the AST path only as a temporary test oracle, then remove it and the dual-engine hooks; move expression-semantics assertions from `internal/query/script_test.go` to `tql` while parser-shape tests stay | Full suite, race, fuzz, golden corpus and benchmark gate pass; one production engine remains |
| 8 | Optimize only with measurements: frame pooling, register reuse, fused path steps, avoiding `NODE`/`ONE` round trips, single-target assignment fast path, constant sharing | Each change has before/after numbers; no parity regressions |

Test corpus and CLI work belong to steps 4–5 and the integration tests in
`integration/`. Estimated core size: roughly 1,500–2,200 lines for the compiler,
VM, target-plan refactor and tooling, plus comparable test volume. The exact count
is not a target; the parity and performance evidence is.

## Risks

| Risk | Mitigation |
| --- | --- |
| Live versus detached values drift | The conversion table above is normative; differential tests include local snapshots, `??`, loops, and `read` |
| Assignment behavior changes | Reuse the existing planning/apply functions; only the target walk moves; parity suite covers creation, filters, slices, aliases, and local owners |
| Error text or limit timing changes | Emit ticks at the same logical events; golden error tests; keep statement-prefix stack |
| Constants become shared mutable state | Constants hold plain values and are materialized per evaluation; no node is stored in the pool |
| Two engines diverge | Time-box the AST oracle; freeze a golden corpus, then delete the oracle in the switch step |
| Register/sequence allocations erase the gains | Frame pool by nesting depth; compile with destination passing; benchmark allocations, not only time |
| `owner` resolution differs with slots | Slot bindings stay unique per tree; scan active scopes innermost-first; keep `runtime_test.go` ownership cases |
| `for` reference rebasing differs | Reuse `nodeRef` and `rebase` unchanged; iterator close truncates exactly like the current `defer` |
| Binary format drifts or becomes unreadable | Stable opcode IDs, explicit format version, deterministic encoding, golden images and a rejection test for every version change |
| Malformed or hostile image panics the loader/VM | Bounded reads before allocation, full structural verifier, runtime bounds/kind checks, loader fuzz; never trust counts or indexes |
| Assembly stops re-assembling | One canonical syntax, labels for every branch, typed constants, round-trip tests on the full corpus; debug comments are never functional |
| Performance claims are cherry-picked | Predeclared workloads and thresholds, `benchstat`, machine/date recorded, no CI wall-clock assertions |
| SIMD work adds complexity without a win | Reuse existing scanners; add SIMD only behind build tags with scalar fallback and a measured gate; otherwise reject explicitly |

## Open questions and recommendations

1. **Keep the AST evaluator as a permanent oracle?** Recommendation: no. Keep it
   through step 6, freeze a golden corpus, then remove it at the switch. Two
   engines are a maintenance cost and a source of silent divergence.
2. **Expose bytecode loading, disassembly and assembly publicly?** Recommendation:
   yes; this is a requested feature, not only an internal debug aid. The API and
   CLI shape are in [bytecode format and tooling](bytecode-format.md#public-api-and-cli).
3. **Compile eagerly in `Compile`?** Recommendation: yes. Lazy compilation adds
   mutable state and concurrency questions to an otherwise immutable `Program`.
4. **Share literal trees across executions?** Recommendation: start by
   materializing; measure. The current engine creates a fresh tree for each
   literal evaluation, and sharing nodes would need proof that no code path
   mutates them in place.
5. **Extract the VM to a separate package now?** Recommendation: no. Statements
   are coupled to `Document` semantics. Tooling is already separated into
   `internal/bytecode`; introduce an execution host boundary only when a second
   host exists.
6. **Guarantee bytecode compatibility across releases?** Recommendation: exact
   format version only. Bump the version on any layout or opcode semantic change,
   reject other versions with "recompile the program", and never guess. Deployed
   programs are expected to be produced by the same TQL version.
7. **What debug information goes into the image?** Recommendation: parameters,
   slot names, statement indexes and sub-code names are functional and mandatory.
   Do not embed the TQL source text or document data. Source byte ranges can be an
   optional later section if a stepping trace needs them.
8. **How stable is the assembly text?** Recommendation: the canonical form is
   versioned with the bytecode; comments are ignored; directives and labels are
   exact. Debug output may add comments but must never rely on them for meaning.
9. **Where is SIMD justified?** Recommendation: reuse the existing scanner SIMD;
   prototype only long-string leaf operations if profiling shows a win; do not
   vectorize dispatch, pointer streams or `math/big` arithmetic. See the matrix in
   [performance and SIMD](bytecode-performance.md#simd-applicability).

## Definition of done

- `tql.Compile` produces immutable bytecode; `Document.Execute` runs it.
- The AST evaluator is no longer on the production path.
- All existing tests, recipes, fuzz seeds, race tests, and the golden corpus pass.
- Error strings, step limits, rollback, ownership, parameters, and concurrency
  behavior match the current engine.
- `MarshalBinary`/`LoadProgram` persist and validate a versioned image; malformed
  input fails with a clear error and never panics.
- `Disassemble`/`Assemble` round-trip the full test corpus bit-for-bit at the byte
  level, and assembled programs execute identically.
- The A/B benchmark gate is met and reported with machine, Go version, commands
  and raw results; otherwise the switch did not happen.
- The SIMD audit is complete: existing scanners reused, prototype candidates
  measured, and rejected candidates documented with a reason.
- CLI help, `docs/architecture.md`, `docs/performance.md`, and `CHANGELOG.md`
  describe the execution model, persistence format, tools and measured effect.
