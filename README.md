# TQL — Tree Query Language

[![CI](https://github.com/vasileknik76/tql/actions/workflows/test.yml/badge.svg)](https://github.com/vasileknik76/tql/actions/workflows/test.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/vasileknik76/tql.svg)](https://pkg.go.dev/github.com/vasileknik76/tql)
[![Release](https://img.shields.io/github/v/release/vasileknik76/tql)](https://github.com/vasileknik76/tql/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Pronounced **tequila**.

Read and edit JSON, TOML, and YAML without reformatting the rest of the file.
TQL is a Go library and command-line tool with a jq-like query language, exact
numbers, and transactional migration scripts. It has no third-party dependencies.

```sh
tql 'server.port' config.toml
tql -i 'server.port = 9090' config.toml
tql -i 'users[name == "alice"].enabled = true' config.yaml
```

## Preserve the source

A scalar edit replaces only the value's bytes. Given this TOML file:

```toml
# Production
port   = 8080  # public port
label  = 'keep this spelling'
```

`tql 'port = 9090' config.toml` produces:

```toml
# Production
port   = 9090  # public port
label  = 'keep this spelling'
```

TQL keeps the original source and applies targeted edits instead of serializing the
whole document. Unaffected comments, key order, whitespace, string escapes, and
LF/CRLF line endings stay intact. A semantically equal assignment leaves the
original bytes unchanged, including a number's spelling (`1` versus `1.0`).

New entries follow the surrounding style where possible: indentation, separators,
quotes, line endings, and collection layout. TQL uses nearby entries as examples
and falls back to a valid default when the source provides no clear convention.
Existing entries and comments stay in place. YAML and TOML support explicit
[per-kind generation policies](docs/style.md) through CLI flags and `tql.WithStyle`;
the default is `auto`.

Insertions and deletions may also change the punctuation, indentation, or table
headers needed to keep the document valid. Replacing a whole object or array can
replace formatting and comments inside that subtree. See the
[editing contract](docs/architecture.md) for the precise preservation boundaries.

## Install

Download a prebuilt binary for your operating system and architecture from
[GitHub Releases](https://github.com/vasileknik76/tql/releases/latest). No Go
installation is needed to run it. Each release includes SHA-256 checksums.

To install from source, use **Go 1.23 or later**:

```sh
go install github.com/vasileknik76/tql/cmd/tql@latest
```

Use `@v0.1.0` instead of `@latest` to pin the first release. See the
[installation guide](docs/cli.md#installation) for release archive names and
commands for Linux, macOS, and Windows.

To build from a checkout:

```sh
go build -o bin/tql ./cmd/tql
./bin/tql 'server.port' examples/config.toml
./bin/tql 'server.port = 9090' examples/config.toml
./bin/tql --query-file examples/migrate.tql examples/config.yaml
```

Edits print the resulting document by default. Add `-i` to update the input file;
use `--check` to test whether an edit would change its bytes without writing it.

## Query and edit

| Query | Result |
| --- | --- |
| `foo.bar` | Read a field |
| `baz[1].name = 3` | Update an array element |
| `obj = [1,2,3]` | Replace a value |
| `objs[name == "foo"] = "abc"` | Replace each matching element |
| `objs[name == "foo"].enabled = true` | Set a field on matching objects |
| `del(foo.bar)` | Delete a field |
| `del(items[0], items[-1])` | Delete selected array elements |
| `items[1:3].price *= 2` | Update an array slice |
| `items[].price \|= . + 10` | Compute each price from its current value |
| `server.timeout ??= 30` | Set a missing or `null` value |
| `users[] \| select(.active) \| .name` | Produce a stream of values |
| `port = 9000; debug = false` | Apply several edits in order |

TQL is inspired by jq, but is not jq-compatible. Filters can appear directly inside
`[...]`, statements use semicolons, and decimal arithmetic does not silently round
nonterminating fractions. See the [language reference](docs/syntax.md).

## Practical configuration migrations

The [recipe collection](examples/recipes/README.md) includes realistic files,
idempotent migration scripts, and exact expected output:

| Configuration | Example migration |
| --- | --- |
| [Python `pyproject.toml`](examples/recipes/python-release/README.md) | Update package metadata and a selected dependency while keeping comments and layout |
| [Go `.golangci.yaml`](examples/recipes/go-linters/README.md) | Enable missing linters without duplicates and adjust the timeout |
| [Compose `compose.yaml`](examples/recipes/compose-rollout/README.md) | Update selected services and add logging limits using local document style |
| [Node `package.json`](examples/recipes/node-release/README.md) | Update release metadata, engines, and scripts; remove an obsolete hook |
| [OpenAPI 3.0 → 3.1](examples/recipes/openapi31/README.md) | Migrate nullable and exclusive schema bounds while preserving layout |

For example, preview the Python migration against the included fixture:

```sh
tql --query-file examples/recipes/python-release/migrate.tql examples/recipes/python-release/pyproject.toml
```

Add `-i` to save a migration to an input file. Each recipe explains its assumptions
and shows the resulting diff.

## Write migration scripts

```tql
let $enabled = 0;
for $user in users[] {
    if $user.name == "alice" {
        $user.enabled = true;
        $enabled += 1;
    }
}
$enabled
```

```sh
tql -i --query-file migration.tql config.yaml
tql --values --query-file migration.tql config.yaml
```

Scripts support lexical variables, conditions, loops, live node references,
dynamic indexes, and recursive traversal with `walk()`. If any statement fails,
the entire execution is rolled back. The [scripting guide](docs/scripts.md)
explains scopes, iteration, and execution limits.

Declare inputs to reuse a script with different parameters:

```tql
param $service;
param $image;
param $replicas = 2;

services[$service].image = $image;
services[$service].deploy.replicas = $replicas;
```

```sh
tql --arg service=api --arg image=api:v2 --argjson replicas=3 \
    --query-file rollout.tql compose.yaml
```

Parameters are read-only values, checked before execution. Use `--params-file`
for a JSON object of inputs, or `tql.WithParams` with the Go execution APIs.
See [script parameters](docs/scripts.md#input-parameters) for defaults and scope.

## Compile once, run or inspect later

```sh
tql --query-file migration.tql --emit-bytecode migration.tqlc
tql --program migration.tqlc config.yaml
tql --program migration.tqlc --disassemble
tql --query-file migration.tql --emit-assembly migration.tqla
tql --assemble migration.tqla config.yaml
```

A bytecode image is versioned and validated on load. Disassembly is canonical
and can be assembled back; it is a debugging and review format, not a required
deployment step. A complete `.tql`/`.tqla`/`.tqlc` walkthrough is in
[examples/bytecode](examples/bytecode/README.md). The Go API adds
`Program.MarshalBinary`, `tql.LoadProgram`, `Program.Disassemble` and
`tql.Assemble`; see the [bytecode design](docs/bytecode-vm.md) and
[format notes](docs/bytecode-format.md).

## Use the Go library

```go
package main

import (
    "fmt"
    "log"

    "github.com/vasileknik76/tql"
)

func main() {
    doc, err := tql.Parse([]byte("port = 8080 # keep\n"), "toml")
    if err != nil {
        log.Fatal(err)
    }
    result, err := doc.Exec(`port = 9090; port`)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Print(string(result.Bytes)) // port = 9090 # keep
}
```

Use `Query` for reads, `Exec` for transactional scripts, and `Compile`/`Execute` to
reuse a program across documents. New formats implement `syntax.Format` and can
be used directly or registered in an isolated registry. See the
[Go API guide](docs/library.md).

## Supported formats

| Format | Support |
| --- | --- |
| JSON | RFC 8259, exact numbers, Unicode, and any root value. Duplicate keys and invalid surrogate sequences are rejected. |
| TOML | TOML 1.1, including dotted keys, tables, arrays of tables, multiline inline tables, strings, dates, and numbers. |
| YAML | YAML 1.2 syntax graphs and document streams, with queries and source-preserving edits for representable value trees. See [YAML APIs and limits](formats/yaml/README.md). |

Each format has a source-aware parser written in Go. Unsupported syntax or an
edit that the format cannot represent returns an error. See
[format compatibility](docs/formats.md).

The parsers pass 1,401 upstream test cases in CI: 681 from
[toml-test](https://github.com/toml-lang/toml-test), 402 from the official
[YAML Test Suite](https://github.com/yaml/yaml-test-suite), and 318 from
[JSONTestSuite](https://github.com/nst/JSONTestSuite). The pinned corpora run as part
of `go test ./...`. JSON cases follow TQL's stricter duplicate-key policy; see
[parser conformance](conformance/README.md) for versions and exact expectations.

## Documentation

- [CLI usage and file handling](docs/cli.md)
- [Language reference](docs/syntax.md)
- [Scripting guide](docs/scripts.md)
- [Go API and custom formats](docs/library.md)
- [Structured editing, diff, and merge from Go](docs/edit.md)
- [Architecture and source preservation](docs/architecture.md)
- [Bytecode VM](docs/bytecode-vm.md)
- [Bytecode format, persistence, and assembly](docs/bytecode-format.md)
- [Bytecode performance and SIMD](docs/bytecode-performance.md)
- [Performance](docs/performance.md)
- [File integration tests](docs/integration-tests.md)
- [Parser conformance and upstream test suites](conformance/README.md)
- [Runnable examples](examples/README.md)
- [Practical configuration recipes](examples/recipes/README.md)

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for local checks and testing guidelines.

```sh
go test ./...
go test -race ./...
go vet ./...
go test ./formats/json -run '^$' -fuzz Fuzz -fuzztime 10s
go test ./internal/query -run '^$' -fuzz FuzzParse -fuzztime 10s
go test -bench . -benchmem .
```

Tests cover exact output bytes, invalid syntax, transactional edits, real CLI file
operations, and parser fuzzing. Independent replacements within one statement are
batched to avoid reparsing once per target; see the [performance guide](docs/performance.md)
for benchmarks and the cost of sequential scripts.

Changes are recorded in the [changelog](CHANGELOG.md). Report security issues using
the [security policy](SECURITY.md).

## License

[MIT](LICENSE).
