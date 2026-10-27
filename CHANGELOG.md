# Changelog

## Unreleased

- Execute compiled TQL programs on a register bytecode VM, with lexical slots,
  materialized stream/scalar operands and the existing transactional edit engine.
- Add versioned bytecode images with `Program.MarshalBinary` and
  `tql.LoadProgram`, including bounded decoding, structural validation and fuzz
  coverage.
- Add canonical disassembly and assembly through `Program.Disassemble`,
  `tql.Assemble`, `--disassemble`, `--assemble`, `--program` and
  `--emit-bytecode`.
- Preserve exact source bytes, errors, limits, scopes, live references and
  parameter behavior; expression-heavy workloads are faster and edit-dominated
  workloads remain neutral. See `docs/bytecode-performance.md`.
- Add a SIMD rune-count fast path for `length(string)` on Go 1.27+ with
  `GOEXPERIMENT=simd`, retaining scalar and `purego` fallbacks.
- Reduce VM overhead with scalar truth predicates, specialized key/literal
  filters, primitive equality fast paths, single-node path fast paths, integer
  arithmetic before exact rational arithmetic, read-only loop bindings, direct
  scalar reads and pooled execution frames.

## v0.4.0

- Add reusable script parameters with `param`, defaults, and read-only values,
  supplied through CLI flags or `tql.WithParams` in Go.

## v0.3.0

- Add optional Go 1.27 SIMD scanning for JSON, TOML, YAML and query strings on
  arm64 and amd64, enabled with `GOEXPERIMENT=simd`.
- Reduce allocations in string parsing, quoting and insertion style selection,
  and avoid repeated buffer copies when folding multiline YAML strings.
- Infer table, inline and dotted object conventions when adding TOML values,
  including arrays of tables and nested headers within a selected array element.
- Add document-local `tql.WithStyle` and CLI generation policies for objects,
  arrays, strings, keys, integers, indentation and line endings, defaulting to
  `auto`. Preserve value semantics when a preferred spelling is unsuitable.

## v0.2.0

- Add a structured Go editing API with typed paths, atomic `Set`/`Del` patches,
  document diff and merge planning, and optional TQL script export.
- Extend the TOML parser and editor to TOML 1.1, including multiline inline
  tables, the new string escapes, and times without seconds.
- Add a source-aware YAML 1.2 graph and document-stream API that preserves tags,
  anchors, aliases, complex keys, and recursive graph edges.
- Accept colons before flow punctuation inside YAML block plain scalars, including
  unquoted template tags such as `pod:{{ .pod }}`. Flow collection restrictions
  remain unchanged.

## v0.1.0

First public release of TQL — Tree Query Language, pronounced tequila.

- Go library and CLI for reading and editing JSON, TOML, and a configuration-oriented
  YAML 1.2 profile, using handwritten source-aware parsers without dependencies.
- Targeted byte edits that retain surrounding comments, whitespace, ordering, and
  scalar spelling. Insertions infer local indentation, quoting, and collection style.
- jq-like paths, filters, streams, assignments, deletion, and exact decimal arithmetic.
- Transactional scripts with lexical variables, conditions, loops, and recursive
  traversal. Failed executions roll back all document changes.
- Atomic in-place file updates, stdin/stdout operation, script files, and check mode.
- Extensible format API, reusable compiled programs, unit and file integration
  tests, fuzz targets, and benchmarks.
- Tested, idempotent migration recipes for Python packaging, Go linters, Compose
  deployments, and Node packages.
- Prebuilt CLI binaries and SHA-256 checksums for multiple operating systems and
  architectures.

TQL is not jq-compatible. YAML support is an explicit configuration profile rather
than the complete specification. Replacing an entire container can replace its
internal formatting and comments. See [format support](docs/formats.md) and the
[editing contract](docs/architecture.md#byte-preservation) for the boundaries.
