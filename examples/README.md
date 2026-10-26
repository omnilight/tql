# Examples

Build the CLI from the repository root:

```sh
go build -o bin/tql ./cmd/tql
```

The three `config` files represent the same value tree in JSON, TOML, and YAML.
Use the same query or script with any of them:

```sh
./bin/tql 'users[name == "alice"].enabled' examples/config.toml
./bin/tql --query-file examples/update.tql examples/config.toml
./bin/tql --query-file examples/update.tql examples/config.yaml
./bin/tql --query-file examples/update.tql examples/config.json
./bin/tql --values 'server.port = 9090; server.port' examples/config.yaml
```

These commands leave the example files unchanged. A modifying query prints the
updated document by default; add `--in-place` to save it to the input file.

`update.tql` changes the server port, enables Alice, and adds a timeout when missing.
`migrate.tql` demonstrates variables, conditions, iteration, and recursive traversal:

```sh
./bin/tql --query-file examples/migrate.tql examples/config.yaml
./bin/tql --values --query-file examples/migrate.tql examples/config.toml
```

The migration adds 10 to the enabled server's port, updates selected users, and
counts numeric nodes. With `--values`, it prints the count; without it, it prints
the updated document.

## Configuration recipes

For migrations against real configuration formats, see the
[recipe collection](recipes/README.md):

- [Python packaging](recipes/python-release/README.md): `pyproject.toml` package
  metadata and a selected dependency.
- [Go linting](recipes/go-linters/README.md): `.golangci.yaml` timeout and missing
  linters, without duplicate entries.
- [Compose deployment](recipes/compose-rollout/README.md): selected services and
  logging limits.
- [Node releases](recipes/node-release/README.md): `package.json` engines, version,
  scripts, and obsolete hook removal.

Each recipe contains its input, an idempotent `.tql` migration, and the exact
expected file. Tests execute every recipe and check that a second execution leaves
the result unchanged.

See [TQL syntax](../docs/syntax.md), [scripts](../docs/scripts.md), and
[CLI usage](../docs/cli.md) for more examples.
