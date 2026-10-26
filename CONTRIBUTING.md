# Contributing

Use Go 1.23 or later. TQL has no third-party runtime dependencies. Run commands
from the repository root.

Before sending a change, format Go files and run the checks:

```sh
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go build -o bin/tql ./cmd/tql
```

The integration package builds and executes the real CLI against isolated copies
of fixture files; `go test ./...` includes it. See
[integration testing](docs/integration-tests.md) for what it checks. Additional
linting and vulnerability checks are defined in the
[CI workflow](.github/workflows/test.yml). To run these locally, install the pinned
development tools and put Go's binary installation directory on your `PATH`:

```sh
make tools
make lint
make vuln
make workflow-lint
```

These tools may require a newer Go version than the library's Go 1.23 minimum;
CI runs them with Go 1.26. `make check` runs formatting verification, `go vet`,
and the race-enabled test suite. `make fuzz FUZZTIME=10s` runs all bounded fuzz
checks, and `make oracle` runs the TOML differential harness with Python 3.11+.

For parser and editor changes, add focused tests with exact input and output bytes
as well as decoded values. Include comments, line endings, escaping, or malformed
input when they matter. Preserve no-op behavior and transaction rollback. Existing
fuzz targets can explore parser and editing boundaries:

```sh
go test ./formats/json -run '^$' -fuzz Fuzz -fuzztime 10s
go test ./internal/query -run '^$' -fuzz FuzzParse -fuzztime 10s
```

Keep discovered failures as regression tests. The optional
[TOML differential harness](formats/toml/README.md#differential-testing) compares
the parser with Python's `tomllib`. The optional [YAML suite harness](formats/yaml/README.md#conformance-checks)
compares graph events and values with an external official data export:
`make yaml-suite YAML_TEST_SUITE=/path/to/yaml-test-suite-data`.
For performance work, use the workloads and
measurement guidance in [performance](docs/performance.md).

Document language or API changes with runnable examples. Format-specific behavior
belongs in the backend documentation and [format compatibility guide](docs/formats.md).
New formats implement the existing `syntax.Format` contract; see the
[library guide](docs/library.md#add-a-format).

Open an issue or pull request with the problem, intended behavior, and relevant
tests. Include a minimal input and query when reporting a bug. Remove credentials
and private configuration data from examples and fixtures. Follow the
[security policy](SECURITY.md) for vulnerabilities.

## Releases

`make release VERSION=v0.1.0` cross-compiles the CLI and creates release archives
and `SHA256SUMS` in `dist/`. The builder rejects an existing output directory; use
a fresh directory or remove old build output before rebuilding. It embeds the
version and packages a README and license with each binary. See
[CLI installation](docs/cli.md#prebuilt-binaries) for the target matrix and names.

The [release workflow](.github/workflows/release.yml) runs on `v*` tags. It runs
the full CI workflow before building archives, verifies checksums and the native
binary, and publishes the GitHub release with its assets. Tags containing a
hyphen are marked as prereleases. Only publish a release tag after its commit and
changelog have been reviewed.

Contributions are provided under the repository's [MIT license](LICENSE).
