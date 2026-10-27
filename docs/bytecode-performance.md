# Bytecode performance and SIMD

The VM is the only execution engine. The AST interpreter was removed after the
VM became the production path; the comparison below is the measured result from
that migration and is historical. Edit-heavy work stays neutral because parsing,
editing, reparsing and verification dominate it.

## Measured results

Apple M3 Pro, macOS arm64, Go 1.27.1, 2026-10-28. Medians of three 500 ms runs;
the AST column is historical and no longer reproducible from the repository.

| Workload | VM | Previous AST | VM advantage | Allocations (AST → VM) |
| --- | ---: | ---: | ---: | ---: |
| Compiled read, 1,000 services | ~19 µs | ~221 µs | ~91% faster | 7,015 → 4 |
| Filter + project, 1,000 | ~41 µs | ~135 µs | ~70% faster | 7,044 → 16 |
| Read loop, 10 items | ~1.1 µs | ~4.4 µs | ~76% faster | 183 → 24 |
| Nested read loops, 10 × 10 | ~9.4 µs | ~45 µs | ~79% faster | 1,689 → 143 |
| `walk` update | ~10 µs | ~13 µs | ~23% faster | 456 → 297 |
| Mutation loop, 128 items | ~2.3 ms | ~2.6 ms | near neutral | 62,651 → 61,104 |
| One-shot edit, 1,000 services | ~1.07 ms | ~1.07 ms | neutral | ~40,100 |
| OpenAPI 3.0→3.1 check, 50 schemas | ~0.60 ms | ~1.13 ms | ~47% faster | 31,272 → 15,821 |
| OpenAPI 3.0→3.1 apply, 50 schemas | ~26 ms | ~25 ms | neutral | 686,789 → 670,992 |

Read-heavy rows are stable in direction but vary with machine noise. Edit-heavy
rows are neutral, not a reliable fine-grained timing signal. `LoadProgram` is
about 2.3 µs for the benchmark program versus about 3.2 µs for `Compile`
(99 → 39 allocations).

## Workloads

| Benchmark | What it exercises |
| --- | --- |
| `CompiledRead` | Key, filter, key over 4,096/1,000-value trees |
| `ScriptFilter` | Filter and project pipeline over services |
| `ScriptReadLoop` | Loop dispatch and scalar reads without edits |
| `ScriptNestedReadLoops` | Nested control flow and frame reuse |
| `ScriptWalk` | Tree traversal with a numerical update |
| `ScriptForLoop` | Per-item edit planning and reparsing |
| `OneShotEdit` | Parse + one edit + reparse + verify |
| `OpenAPI31Check` | Read-only schema-rule scan over 50 generated OpenAPI schemas |
| `OpenAPI31` | Apply the OpenAPI 3.0→3.1 recipe to the same document |

The benchmark names use the `VM` prefix and run against the only engine.

## How to reproduce

```sh
make bench-bytecode > bench-vm.txt
go test -run '^$' -bench 'Benchmark(CompileProgram|LoadProgram|AssembleProgram)' \
  -benchmem -count=10 . > bench-tools.txt
```

Compare saved runs with `benchstat` (a development tool, not a module
dependency). Record CPU, OS, Go version, commit, date and `GOMAXPROCS`, and use
more than one session. Do not put absolute wall-clock thresholds in CI.

## Where time goes

- **Reads and filters:** after specialization, `match` and `applyPath` dominate.
  A simple key/literal filter no longer builds a boolean node or a sub-code
  frame per element.
- **Mutation loops:** the edit pipeline, not the VM. Each iteration reparses and
  re-verifies the document; `syntax.Node.Interface`, JSON parsing and
  `semanticChange` are visible in profiles.
- **`walk`:** traversal plus per-node edit work. A read-only walk is much cheaper
  than a mutating one.
- **One-shot:** document parsing and format verification dominate. Code
  generation and VM setup are small next to that.

This is why further VM micro-optimization has high leverage on reads/filters and
limited leverage on migrations.

## SIMD applicability

TQL already uses SIMD for JSON, TOML, YAML and query-string scanning on Go 1.27+
with `GOEXPERIMENT=simd`, plus scalar and `purego` fallbacks. The VM reuses those
scanners.

The VM loop itself is a poor SIMD target: dispatch is a small branch-heavy
switch, streams move pointers, `walk` is pointer-chasing, and arithmetic uses
exact decimal logic. SIMD is appropriate only for leaf scans.

| Candidate | Decision |
| --- | --- |
| Parser/scanner byte loops | Reuse existing SIMD |
| `length(string)` rune count | Implemented: continuation-byte count with scalar tail and `utf8.ValidString` fallback; about 1.0 µs versus 3.9 µs for a 14 KiB string |
| `startswith`/`endswith`/`contains` | Use stdlib; its assembly is already optimized; benchmark before replacing |
| Regex `test` | Regex engine dominates; no SIMD |
| `Equal` on arrays/interfaces | Only prototype with a real workload; pointer/interface traversal limits gains |
| Stream filter/copy, `sort`, `reverse` | Memory/comparison bound; reject |
| Number arithmetic | `math/big`/exact decimal logic; reject |
| Object lookup | Maps/indexes already O(1); not a vector problem |
| Bytecode decode | Small fixed fields; bounds checks dominate; reject |

New SIMD code must use the existing build tags
`go1.27 && goexperiment.simd && !purego && (amd64 || arm64)`, keep a scalar
fallback, and show at least a 10% gain on a real workload with no short-input
regression.

## Guardrails

- Correctness parity comes before timing.
- Use predeclared workloads, record the environment, and compare medians.
- Do not run absolute time assertions in CI; keep correctness gates there.
- If a change does not beat the noise on a representative workload, leave it out
  and document the rejection.
