# OpenAPI 3.0 → 3.1 schema migration

This recipe demonstrates a source-preserving migration of the common OpenAPI
3.0 → 3.1 schema changes:

- bump `openapi` to `3.1.0`;
- replace `nullable: true` with a JSON Schema type union such as
  `type: ["string", "null"]`;
- drop `nullable: false`;
- turn boolean `exclusiveMinimum`/`exclusiveMaximum` with their bound into the
  3.1 numeric form.

Preview the migration:

```sh
tql --query-file examples/recipes/openapi31/migrate.tql \
  examples/recipes/openapi31/openapi.yaml
```

Check or apply:

```sh
tql --check --query-file examples/recipes/openapi31/migrate.tql \
  examples/recipes/openapi31/openapi.yaml

tql -i --query-file examples/recipes/openapi31/migrate.tql \
  examples/recipes/openapi31/openapi.yaml
```

The expected result is in [`expected/openapi.yaml`](expected/openapi.yaml).
Comments, ordering and unrelated formatting stay in place.

## Check before applying

`check.tql` reports the constructs the migration would change without editing
the document:

```sh
tql --values --query-file examples/recipes/openapi31/check.tql \
  examples/recipes/openapi31/openapi.yaml
```

The output is one JSON object per finding, with a document path and rule name.
This is a useful CI preflight and a read-only OpenAPI workload.

## What the script assumes

The fixture keeps schemas under `components.schemas`. The script uses `walk` over
that subtree, so it is a recipe-shaped migration rather than a complete OpenAPI
transformer.

It assumes:

- a nullable schema has a `type` string to extend;
- `exclusiveMinimum`/`exclusiveMaximum` are boolean 3.0 keywords with the matching
  `minimum`/`maximum`;
- the input is one YAML document.

It deliberately does not resolve multi-file `$ref`, rewrite every inline schema,
migrate `example`/`examples` usage, or validate the result against the complete
OpenAPI schema. A production transformer should add schema-aware traversal,
`$ref` graph handling and a versioned rule catalog.

## From this recipe to a full migrator

The recipe is intentionally small enough to stay reviewable. A production
`tql-openapi31` program would keep the same editing core and add:

- a rule catalog with `detect`, `plan` and `apply` phases, versioned per rule;
- schema-aware traversal instead of a broad `walk`, so example payloads and
  extensions are not mistaken for schemas;
- `$ref` resolution across files and documents, with each referenced file
  edited through TQL and checked for cycles;
- `check`/`plan`/`apply` commands, a migration report and a stable exit status;
- validation of the result against the supported OpenAPI/JSON Schema rules;
- golden fixtures for each rule and idempotence checks.

The recipe shows the part that benefits most from TQL: precise, local edits that
keep comments, key order and surrounding formatting intact.

## Bytecode workflow

The same recipe works through all three program forms:

```sh
tql --query-file examples/recipes/openapi31/migrate.tql \
  examples/recipes/openapi31/openapi.yaml

tql --query-file examples/recipes/openapi31/migrate.tql \
  --emit-bytecode /tmp/openapi31.tqlc
tql --program /tmp/openapi31.tqlc \
  examples/recipes/openapi31/openapi.yaml

tql --query-file examples/recipes/openapi31/migrate.tql \
  --emit-assembly /tmp/openapi31.tqla
tql --assemble /tmp/openapi31.tqla \
  examples/recipes/openapi31/openapi.yaml
```

## Performance

The recipe is tested on a generated OpenAPI document with 50 schemas:

```sh
go test -run '^$' -bench 'Benchmark(AST|VM)OpenAPI31' -benchmem .
go test -run '^$' -bench 'Benchmark(AST|VM)OpenAPI31Check' -benchmem .
```

On the reference machine the read-only check is about 45% faster on the VM
(~2.3 ms → ~1.2 ms, 31k → 16k allocations). Applying the migration is neutral:
both engines spend most time reparsing and verifying after each edit, not in the
VM loop. This is expected for a source-preserving migration and is exactly the
kind of end-to-end workload that should accompany the microbenchmarks in
[bytecode performance](../../../docs/bytecode-performance.md).
