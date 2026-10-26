# Changelog

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
