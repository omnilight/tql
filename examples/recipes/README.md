# Practical configuration recipes

Use TQL to make small, reviewable changes to familiar project files. Each recipe
contains a realistic input, an executable `migrate.tql`, and the exact expected
output. The examples are tested through the Go API for both the edited bytes and
idempotence; running a migration twice leaves the second result unchanged.

| File | Task | Features |
| --- | --- | --- |
| [pyproject.toml](python-release/README.md) | Prepare a release and update one dependency constraint | Quoted keys, predicates, preservation of inline comments |
| [.golangci.yaml](go-linters/README.md) | Adopt extra linters without duplicating existing entries | Variables, conditions, loops, append at array length |
| [compose.yaml](compose-rollout/README.md) | Update a labelled group of services and their log limits | Object traversal, live references, selective changes |
| [package.json](node-release/README.md) | Update release metadata and modernize npm hooks | Assignment, insertion, deletion, local indentation |

Commands in each recipe assume that `tql` is on `PATH` and that the current
directory is this repository's root. Omit `-i` to preview the edited document;
add `-i` to apply a script to a file. Use `--check` to detect pending changes
without writing: exit status `1` means changes are needed and `0` means the
file already matches the migration; status `2` is an error.

```sh
tql --check --query-file examples/recipes/go-linters/migrate.tql \
  examples/recipes/go-linters/.golangci.yaml
```

These migrations intentionally target the documented fixture shapes. Choose
appropriate version constraints and adapt field assumptions before using them
in another project. TQL preserves source formatting; it does not resolve package
dependencies or validate application-specific configuration schemas.

[General examples](../README.md) · [Language reference](../../docs/syntax.md)
