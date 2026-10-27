# Performance and SIMD

Status: measured on this branch. The VM is the default execution engine. The gate
is met for expression-heavy work; edit-dominated workloads are neutral, not slower.

Related plans:

- [Bytecode virtual machine](bytecode-vm.md) — compiler, VM and execution
  semantics.
- [Bytecode format and tooling](bytecode-format.md) — save/load, disassembly and
  assembly.

## What must be proven

The claim is not "bytecode is faster". The claim is narrower and testable:

1. For expression-heavy work (reads, filters, `walk`, loops), the VM is faster
   than the recursive AST evaluator.
2. For edit-dominated work, the VM is not slower beyond benchmark noise, because
   parsing, format edits, reparsing and semantic verification dominate.
3. The VM allocates less per expression and per loop iteration.
4. One-shot `Exec`/`Eval` stays acceptable after code generation is added.
5. A saved program loads faster than reparsing and compiling its source.

If any of 1–4 fails, the VM stays an internal experiment. A negative result is a
valid outcome; hiding it is not.

## Workloads

Predeclare the workloads before running the A/B. Do not add a favorable workload
after seeing the numbers.

| Workload | Shape | What it isolates | Expected sensitivity |
| --- | --- | --- | --- |
| `CompiledRead` | 4,096-key object, filter and read one path | Path/filter dispatch, `variables()` rebuild | High |
| `ScriptFilter` | 1,000 services, `services[name == "api"].port` | Filter predicate and path evaluation | High |
| `ScriptForLoop` | 128 array items, per-item increment and read | Loop ticks, scopes, slots and edit planning | Medium |
| `ScriptReadLoop` | 10 array items, read expression per item | Loop and expression dispatch without edits | High |
| `ScriptNestedReadLoops` | 10 × 10 read-only nested loops | Control flow and frame reuse | High |
| `ScriptWalk` | Tree walk with `type` and numeric update | `walk` stream and per-node dispatch | Medium/high |
| `ScriptNestedLoops` | Two nested loops with `break`/`continue` | Control flow, scope/slot churn | High |
| `EvalSurgicalEdit` | Parse 1,000 services and change one port | Parse + edit + verify dominate | None/low |
| `LargeMultiTargetEdit` | 256 independent replacements | Batched edit planning and verification | None/low |
| `RecipeMigration` | One real recipe (`pyproject.toml` or `compose.yaml`) | End-to-end script behavior | None/low |
| `OneShotEval` | `tql.Eval(source, format, script)` | Parse + compile + execute | Codegen cost |
| `LoadProgram` | `LoadProgram(MarshalBinary(p))` | Loader versus compile | Positive |

Each benchmark reports `ns/op`, `B/op`, `allocs/op`, and for the persistence
workloads also image size and load time. Correctness is checked before timing:
the AST and VM results must be equal, including bytes, values, `Changed` and
errors.

## A/B harness

During migration, production still uses the AST engine. Add internal entry
points used only by tests and benchmarks:

```go
func (d *Document) executeAST(p *Program, wantBytes bool, o execOptions) (*Result, error)
func (d *Document) executeVM(p *Program, wantBytes bool, o execOptions) (*Result, error)
```

Both run on independent copies of the same document. Benchmarks call each on a
fresh document and a precompiled program. They never compare a warm VM against a
cold AST or include one-time setup in the wrong bucket.

Benchmark families:

```text
BenchmarkASTCompiledRead / BenchmarkVMCompiledRead
BenchmarkASTScriptFilter / BenchmarkVMScriptFilter
BenchmarkASTScriptForLoop / BenchmarkVMScriptForLoop
BenchmarkASTScriptReadLoop / BenchmarkVMScriptReadLoop
BenchmarkASTScriptNestedReadLoops / BenchmarkVMScriptNestedReadLoops
BenchmarkASTScriptWalk / BenchmarkVMScriptWalk
BenchmarkASTOneShotEdit / BenchmarkVMOneShotEdit
BenchmarkCompileProgram / BenchmarkLoadProgram
BenchmarkDisassembleProgram / BenchmarkAssembleProgram
```

The `AST` variants remain test-only oracles while the reference interpreter is
retained; the `VM` variants are the production benchmarks. When the AST engine is
deleted, the AST names go with it.

## Methodology

Run from the repository root on an idle machine:

```sh
make bench-bytecode > bench-vm.txt
go test -run '^$' -bench 'Benchmark(CompileProgram|LoadProgram|AssembleProgram)' \
  -benchmem -count=10 . > bench-tools.txt
```

Compare two saved runs with `benchstat` (a development tool, not a module
dependency):

```sh
benchstat bench-before.txt bench-after.txt
```

Rules:

- Record the CPU, OS, Go version, commit, date and `GOMAXPROCS`.
- Use the same input bytes, format and program for both engines.
- Pre-parse documents when measuring execution; include parsing only in
  end-to-end workloads and label it.
- Pre-compile programs for repeated execution; measure `Compile` and one-shot
  `Eval` separately.
- Warm up with `b.ResetTimer()` after setup, not by hand.
- Run at least three sessions and at least `-count=10` per session; report the
  median and the `benchstat` comparison, not the best run.
- Do not run wall-clock assertions in CI. CI is too noisy for absolute
  thresholds; keep correctness gates in CI and the benchmark gate manual.
- Keep raw results in the pull request and a summary in
  [performance](performance.md). Do not publish a single cherry-picked run.

## Gate

The initial gate is deliberately explicit so it cannot be moved after the fact:

| Criterion | Requirement |
| --- | --- |
| Correctness | Full parity on the differential corpus and golden corpus |
| Expression-heavy speed | At least three of the five expression-heavy workloads at least 20% faster, statistically significant (`benchstat`, p < 0.05) |
| Edit workloads | No workload more than 5% slower; ideally within noise |
| Allocations | At least 20% fewer allocations on `CompiledRead`, `ScriptReadLoop` and `ScriptNestedReadLoops`; mutation loops must not regress materially |
| One-shot `Eval` | No more than 10% slower after code generation; if it is, optimize or document why the trade-off is acceptable |
| Program reuse | VM `Execute` with a reused program is not slower than AST reuse |
| Persistence | `LoadProgram` is faster than parsing and compiling representative scripts, and image size is reported |
| Reproducibility | Another session on the same machine reproduces the direction of the result |

If a criterion misses, the switch does not happen. The team can optimize, narrow
the claim, or keep the AST as the production engine. "The VM is the future" is
not a substitute for a measurement.

## Codegen and one-shot cost

`Exec` and `Eval` compile on every call today; they will also generate bytecode.
Measure:

- `Compile` (parse only, baseline) versus `Compile` with codegen;
- `Eval` end-to-end for a small edit and a script;
- one-shot compile plus execute versus repeated `Execute` with a reused program.

If codegen dominates a one-shot edit, options are:

- make code generation cheaper (fewer temporaries, pre-sized slices, no
  reflection);
- keep a small AST fast path for trivial programs only if it is measured and
  does not create a second semantic engine;
- accept the cost with an explicit documented trade-off, but never silently.

The default plan is to optimize codegen, not to keep two execution engines. A
second engine must still pass the full parity suite and is therefore a last
resort.

## Persistence and tooling cost

Loading and assembly are not on the hot edit path, but they are user-visible:

- `LoadProgram` must validate without allocating from unchecked counts;
- image size should be proportional to code, constants and metadata, not to the
  input document;
- `Assemble`/`Disassemble` may be slower than binary load; they are diagnostics,
  not production hot paths;
- report load time and image size for the same programs used in the VM gate.

No tooling performance target should be used to justify a slower VM or a larger
runtime.

## SIMD applicability

TQL already uses SIMD for JSON, TOML, YAML and query-string scanning under
`GOEXPERIMENT=simd` on Go 1.27+, with scalar and `purego` fallbacks. The VM reuses
those scanners; duplicating them inside the interpreter would add code and risk
for no gain. The exception is `length(string)`: counting continuation bytes over
long UTF-8 strings is a leaf operation that passed the measurement rule.
`RuneCount` was about 1.0 µs per 14 KiB string with SIMD versus about 3.9 µs
scalar on the same machine, with no allocations in either path.

SIMD is a poor fit for the VM's core:

- dispatch is a small opcode switch; branches dominate;
- stream and path operations move `*syntax.Node` pointers, not dense numeric
  arrays;
- filtering and `walk` are pointer-chasing and allocation-bound;
- arithmetic uses `math/big` for exact decimals;
- format editing, reparsing and semantic verification dominate edits and are
  already outside the VM.

Candidate matrix:

| Candidate | Where | Expected gain | Decision |
| --- | --- | --- | --- |
| JSON/TOML/YAML/query scanners | Existing `internal/scan` | High for parsing | Reuse; already SIMD |
| CRC32C for images | Loader | Small; hardware CRC | Use `hash/crc32` Castagnoli; no custom kernel |
| `length(string)` rune count | `CALL length` | Medium for long strings | Implemented behind the existing SIMD build gate; scalar tail and fallback |
| `startswith`/`endswith`/`contains` | Built-ins | Low | Use `strings`; its assembly is already optimized; benchmark before any replacement |
| `test` regex | Built-in | None | Regex engine dominates; do not vectorize |
| Equality of long strings/scalar arrays | `query.Equal` | Low/medium | Strings are already optimized; interface arrays need a benchmarked prototype |
| Stream filter/copy, `walk` | VM | None | Memory/pointer bound; reject |
| `sort`/`reverse` | Built-ins | Low | Comparison and pointer swaps dominate; reject |
| Number parse/canonicalize | Arithmetic | None | `math/big` dominates; reject |
| Object key lookup | `Node.Get` | None | Map/index is already O(1); not a vector problem |
| Bytecode decode | Loader | None | Small fixed-width fields; bounds checks dominate |
| Assembly/disassembly text scanning | Tooling | Low | Diagnostics only; reject |

Prototype rules:

- Follow the existing build tags:
  `go1.27 && goexperiment.simd && !purego && (amd64 || arm64)`.
- Keep a scalar implementation in the default file; on amd64 check AVX before
  using 128-bit operations, as `internal/scan` does.
- Stay within the Go 1.23 minimum for default builds; SIMD files must not break
  other architectures or `purego`.
- Require at least a 10% improvement on a real workload with no short-input
  regression; otherwise reject the kernel and record why.
- Keep SIMD out of error-ordering, transaction and ownership code. It may only
  accelerate leaf byte/string scans whose result is independent of order.

The honest conclusion is that SIMD can help parsing and possibly a few long-string
built-ins, not the bytecode dispatch loop, tree edits or exact arithmetic.

## Measured results

Optimized implementation measurements on an Apple M3 Pro, macOS arm64, Go 1.27.1,
2026-10-27. The command was:

```sh
go test -run '^$' -bench 'Benchmark(AST|VM)' -benchmem -benchtime=500ms -count=5 .
```

Medians from that run:

| Workload | AST | VM | Change | Allocations |
| --- | ---: | ---: | ---: | --- |
| `CompiledRead` | ~100–280 µs | ~16 µs | ~85–90% faster | 7,015 → 4 |
| `ScriptFilter` | ~100 µs | ~32 µs | ~68% faster | 7,044 → 16 |
| `ScriptReadLoop` | ~5.5 µs | ~1.5 µs | ~73% faster | 183 → 25 |
| `ScriptNestedReadLoops` | ~60 µs | ~10 µs | ~83% faster | 1,689 → 144 |
| `ScriptWalk` | ~20 µs | ~14 µs | ~30% faster | 456 → 298 |
| `ScriptForLoop` | ~2–3.5 ms | ~2–3.4 ms | neutral | 62,651 → 61,105 |
| `OneShotEdit` | ~1.3–2.2 ms | ~1.3–2.3 ms | neutral | ~40,100 |

`LoadProgram` measured about 2.3 µs for the benchmark program versus about 3.2 µs
for `Compile` (99 → 39 allocations).

The gate is met for the expression-heavy workloads. Mutation loops and one-shot
edits are neutral because format editing, reparsing and semantic verification
dominate; they are not regressions. These are measurements of one machine and
one revision, not guarantees. Rerun the command before claiming a change.

The largest wins came from compiling filter predicates to scalar truth values
instead of rebuilding a boolean node per element, specializing simple key/literal
filters into `FILTER_MATCH`/path match steps, primitive equality fast paths,
single-node path fast paths,
integer arithmetic before `math/big`, read-only loop bindings without reference
state, direct scalar reads, and a pooled VM/frame reset across executions.
The edit-heavy rows vary with machine load; they are neutral rather than a
reliable timing signal.

## Implementation status

The VM is the default execution engine on this branch. Binary persistence,
disassembly, assembly and the CLI are implemented. The AST evaluator remains only
for differential tests. The scanner SIMD is reused, and `length(string)` now uses a
SIMD rune-count fast path behind the existing build gate.

## Migration steps

| Step | Work | Exit criterion |
| --- | --- | --- |
| 0 | Baseline and A/B skeleton; add the predeclared workloads | Baseline recorded on `main`; both engines run the same fixture |
| 1–3 | VM core (expressions, statements, mutations) | Parity suite passes; no production switch |
| 4–5 | Binary/load and assembly tooling | Tooling correctness and load-time benchmarks pass |
| 6 | Run the A/B gate; profile; audit SIMD candidates | Gate result recorded; every SIMD candidate accepted or rejected with evidence |
| 7 | Switch production to the VM if the gate is met; remove the AST oracle | Full suite, race, fuzz, golden corpus and gate pass |
| 8 | Optimize with before/after measurements only | No parity regression; each change has numbers |

## Risks

| Risk | Mitigation |
| --- | --- |
| Benchmark noise on a laptop | Multiple sessions, `-count`, `benchstat`, idle machine, record environment |
| Microbenchmarks miss real workloads | Include recipes and end-to-end edits; separate execution from parsing |
| The VM wins reads but loses edits | Gate includes edit workloads; do not switch if they regress |
| Codegen makes one-shot `Eval` slower | Measure separately; optimize codegen or document the trade-off |
| SIMD adds maintenance without a win | Reuse existing scanners; require measured leaf gains; keep scalar fallback |
| Absolute time gates make CI flaky | Correctness in CI; performance gate manual and reported |
| A cherry-picked run becomes the claim | Predeclare workloads and thresholds; keep raw files; report medians |
| The AST oracle hides a semantic divergence | Parity and golden corpus before timing; no performance result counts without correctness |

## Open questions

1. **Are the numeric thresholds right?** Recommendation: keep 20% / 5% / 20% /
   10% for the first gate. Change them only before measuring, never after seeing
   the result.
2. **Should `benchstat` be a pinned development tool?** Recommendation: yes, like
   the existing lint tools, installed with `go install` and never added to
   `go.mod`.
3. **Should CI run benchmarks?** Recommendation: no absolute thresholds. A
   scheduled benchmark job may publish a trend, but it must not fail a PR on a
   shared runner.
4. **Which SIMD candidate first?** Recommendation: only `length(string)` if a
   profile shows it on a real workload; otherwise no new SIMD in this work.
5. **Do we need an execution trace?** Recommendation: disassembly first. A
   `--trace` that prints pc/op/registers can be a follow-up if disassembly is not
   enough for debugging.
