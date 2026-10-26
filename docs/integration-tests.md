# File integration tests

Run the CLI integration suite from the repository root:

```sh
go test ./integration
```

The package builds `cmd/tql` once into a temporary directory and executes the binary
as a separate process for each invocation. Tests copy configurations and `.tql`
scripts from `integration/testdata` into isolated temporary directories. Repository
fixtures remain unchanged. Each invocation has a 10-second timeout, and the package
removes its temporary binary when the suite finishes.

## Coverage

- **Practical recipes:** preview, apply, and repeat the documented Python, Go,
  Compose, and Node migrations against real files. Expected bytes and idempotent
  file identity are checked through the compiled CLI.
- **Exact source edits:** `--query-file` and `-i` across JSON, TOML, and YAML, using
  variables, conditions, loops, and recursive `walk`. Fixtures include Unicode,
  unusual whitespace, comments, and TOML CRLF line endings.
- **File metadata:** permission bits survive an in-place update. Reading and no-op
  assignments preserve file identity, modification time, and source bytes.
- **Insertion style:** new fields and array items follow compact spacing, tabs,
  quoted keys and strings, block indentation, CRLF, and missing final newlines.
  Exact bytes are checked after `-i`; strings resembling booleans, numbers, or
  syntax retain their values and types when queried through the CLI.
- **Rollback:** an error in a later iteration leaves the original file intact,
  emits no partial stdout, and leaves no `.tql-*` temporary files.
- **Check mode:** `--check` returns 0 for no change, 1 for a proposed change, and 2
  for an error, without writing the input.
- **Streams:** stdin/stdout and `--values` work without changing unrelated files or
  creating filesystem entries. Failed executions emit no partial values.
- **Script behavior:** deletion during iteration retains references to the original
  selected nodes; `while`, `break`, `continue`, dynamic indexes and keys, and lexical
  variable scopes work through the actual command boundary.
- **Query compatibility:** assignment, filters, and deletion compose in a script and
  produce the expected values.

Expected documents are compared directly as bytes rather than normalized through a
parser. Tests also query results through the CLI after structural operations whose
exact whitespace is not part of that test's assertion. Format and query unit tests
cover the finer details; this suite checks how those components behave together
with real files and processes.
