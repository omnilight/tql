# Bytecode example

This directory shows the three TQL program forms side by side:

| File | Form |
| --- | --- |
| [`../update.tql`](../update.tql) | Source script |
| [`update.tqla`](update.tqla) | Canonical bytecode assembly |
| [`update.tqlc`](update.tqlc) | Versioned binary image |

The script updates the server port, enables Alice, and adds a timeout when it is
missing. The same script works on every `examples/config.*` fixture.

Build the CLI from the repository root:

```sh
go build -o bin/tql ./cmd/tql
```

Run the source:

```sh
./bin/tql --query-file examples/update.tql examples/config.toml
```

Compile it, then run the image:

```sh
./bin/tql --query-file examples/update.tql --emit-bytecode examples/bytecode/update.tqlc
./bin/tql --program examples/bytecode/update.tqlc examples/config.toml
```

Write the source directly to assembly, inspect the image, or run the checked-in
assembly:

```sh
./bin/tql --query-file examples/update.tql --emit-assembly examples/bytecode/update.tqla
./bin/tql --program examples/bytecode/update.tqlc --disassemble
./bin/tql --assemble examples/bytecode/update.tqla examples/config.toml
```

Assemble to a new image:

```sh
./bin/tql --assemble examples/bytecode/update.tqla \
  --emit-bytecode /tmp/update.tqlc
```

All commands print the updated document. Add `-i` to write the input file; use a
copy if you want to keep the example fixtures unchanged.

## Keeping the artifacts current

The `.tqla` and `.tqlc` files are generated from `../update.tql`. Regenerate them
after changing the example script or the bytecode format:

```sh
./bin/tql --query-file examples/update.tql --emit-assembly examples/bytecode/update.tqla
./bin/tql --query-file examples/update.tql --emit-bytecode examples/bytecode/update.tqlc
```

`go test ./...` checks that the assembly and image are current, load correctly,
and produce the same result as the source.
