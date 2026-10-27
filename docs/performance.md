# Performance

TQL optimizes local edits while preserving the original source. Every successful
edit batch is still reparsed and checked against the expected value tree; timings
include that work unless a benchmark explicitly measures a lower-level operation.

## Bytecode execution

`Compile` produces bytecode and `Document.Execute` runs it on a register VM. The
VM removes per-expression interface dispatch and resolves lexical variables to
slots; parsing, format editing, reparsing and semantic verification are unchanged.

On an Apple M3 Pro, macOS arm64, Go 1.27.1:

- compiled reads are about 85–90% faster than the AST engine, with 4 allocations
  per execution;
- filter/project pipelines are about 68% faster;
- read-only and nested loops are about 73–83% faster;
- `walk` updates are about 30% faster;
- mutation loops and one-shot edits are neutral because document parsing,
  editing, reparsing and verification dominate;
- a read-only OpenAPI 3.0→3.1 schema check is about 45% faster on the VM, while
  applying the same migration is neutral because the edit engine dominates.

Exact workloads, commands, machine noise and SIMD results are in
[bytecode performance](bytecode-performance.md). `length(string)` has a SIMD
rune-count fast path on Go 1.27+ with a scalar fallback.

## Edit batching

A statement such as `services[].port += 1` plans independent replacements against a
single tree snapshot. The engine applies their combined byte edits, parses once,
and verifies the complete semantic tree once. `TestIndependentReplacementsParseOnce`
checks this behavior for 256 targets, including the exact resulting bytes.

Overlapping backend edits fall back to sequential processing. For example, two
children of the same YAML alias can each require replacing the entire alias token.
Insertions, deletions, separate statements, and separate loop iterations can also
need sequential reparsing. For bulk updates, a single multi-target statement is
usually less expensive than one assignment per loop iteration.

With bounded tree depth, target deduplication and overlap detection use sorting in
`O(k log k)` for `k` targets. Independent replacements take approximately
`O(n + k log k)` plus expression evaluation, where `n` is the document size.
Sequential edits can take `O(k × n)`.

## Tree lookup and allocation

`syntax.Link` establishes parent pointers and builds lookup indexes for containers
with at least 32 immediate children. The indexes support object-key lookup,
child-to-entry lookup, and child-to-array-index lookup in average `O(1)` time. Small
containers use linear scans to avoid the cost of extra maps.

Indexes are built eagerly. Reading a linked tree does not populate caches or
perform hidden writes. Call `Link` only while the tree is exclusively owned;
relinking rebuilds its indexes. Format implementations must keep linked trees and
source bytes immutable while evaluating queries or planning edits.

`syntax.Apply` validates ranges and calculates the final size before allocating its
output buffer. It preserves insertion order for edits at the same position and
does not mutate the source, replacement text, or caller's edit slice.

## Running benchmarks

Run these commands from the repository root:

```sh
go test -run '^$' -bench . -benchmem -benchtime=250ms -count=3 ./syntax
go test -run '^$' -bench 'Benchmark(WideObject|LargeMultiTarget|CompiledRead|EvalSurgicalEdit)' \
  -benchmem -benchtime=250ms -count=3 .
go test -run '^$' -bench BenchmarkStyledInsertion -benchmem .
```

The following baseline was recorded during initial development on an Apple M3 Pro,
macOS arm64, with Go 1.26.3. Each workload ran three times with a 250 ms benchmark
window; values are rounded. These are measurements of those workloads on that
machine, not performance guarantees. The baseline predates repository publication
and is not tied to a tagged release; rerun the commands when comparing changes.

| Benchmark workload | Time per operation | Allocation per operation |
| --- | ---: | ---: |
| `Get` and `Entry` for the last of 4,096 fields in an already parsed tree | 11.2 ns | 0 allocations |
| Apply 256 byte replacements to 64 KiB of source | 10.1–10.5 µs | 84,608 bytes; 2 allocations |
| Parse JSON and increment all 256 fields (`.[] += 1`) | 0.62–0.67 ms | About 0.69 MB |
| Read the last of 4,096 JSON fields through `Execute` | 8.6–9.1 µs | 65,896 bytes |
| Parse 1,000 services and edit one port | 1.76–1.80 ms | About 2.91 MB |
| Select a service by name among 1,000 services and read its port | 192–215 µs | About 385,741 bytes |

The lookup microbenchmark excludes parsing and index construction.
`BenchmarkWideObjectParse` measures that cost separately. `BenchmarkWideObjectRead`
includes the public API and independent result-byte copies.
`BenchmarkLargeMultiTargetEdit` includes initial parsing and all 256 updates.
`BenchmarkStyledInsertion` isolates style inference and edit planning in all three
backends, with 32 and 2,048 existing fields. It excludes parsing, applying edits,
and semantic verification so those costs do not obscure style inference.

## Reusing programs

Compile a script once with `tql.Compile`, then pass the resulting program to
`Document.Execute` for each document. This avoids repeated TQL parsing; it does not
skip configuration parsing or post-edit verification. Independent documents can
run concurrently with the same compiled program.

TQL is not a streaming processor. Memory usage grows with source size, tree size,
selected targets, and output values. Parsing and traversal can dominate a single
edit or filter, so improvements to batched updates should not be assumed to improve
all workloads equally.

## SIMD builds

Go 1.27 can use the experimental SIMD scanners on arm64 and amd64:

```sh
GOEXPERIMENT=simd make build
```

Other Go versions and architectures use scalar scanners. On amd64, SIMD also
requires AVX support. Build with `-tags=purego` to force the scalar implementation.
The minimum supported Go version remains 1.23.
