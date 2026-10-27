# jq programs ported to TQL

A few well-known jq patterns translated to TQL and checked against the same
input. TQL is not a jq implementation; the goal is to show which patterns map
cleanly to source-preserving TQL and which need new language features.

Sources inspected:

- [jq manual](https://jqlang.org/manual/) — canonical filters and built-ins;
- [jq-stable-recipes](https://github.com/Felix-El/jq-stable-recipes) — canonical
  ordering and deterministic output;
- [jq_cookbook](https://github.com/satisfice/jq_cookbook) — practical analysis
  scripts, including `find-key`;
- [jq cookbook](https://github.com/textarcana/jq-cookbook) — ETL-style examples.

The ported programs were checked against jq 1.8.2 on the same input. jq builds
new values, while TQL edits the source, so a jq filter may print only a subtree
where TQL prints the whole edited document.

## Ported examples

Run from the repository root.

| jq | TQL | Result |
| --- | --- | --- |
| `.users[] \| select(.age > 30) \| {name, age}` | [`select.tql`](select.tql) | Two projected objects |
| `.. \| objects \| select(has("name")) \| .name` | [`walk.tql`](walk.tql) | Three names |
| `.settings \| map_values(. + 1)` | [`map-values.tql`](map-values.tql) | Updated document |
| `walk(if type == "object" then del(.password) else . end)` | [`delete-secrets.tql`](delete-secrets.tql) | Passwords removed |
| `paths as $p \| select($p[-1] == "name") \| {path: $p, value: getpath($p)}` | [`find-key.tql`](find-key.tql) | Three `{path, value}` objects |
| `reduce .nums[] as $x (0; . + $x)` | [`sum.tql`](sum.tql) | `10` with `--values` |

```sh
tql --query-file examples/jq/select.tql examples/jq/input.json
tql --query-file examples/jq/walk.tql examples/jq/input.json
tql --query-file examples/jq/map-values.tql examples/jq/input.json
tql --query-file examples/jq/delete-secrets.tql examples/jq/input.json
tql --query-file examples/jq/find-key.tql examples/jq/input.json
tql --values --query-file examples/jq/sum.tql examples/jq/input.json
```

The mutation examples print the edited document; `sum.tql` returns a read value,
so it needs `--values`. In `map-values.tql`, jq returns only `.settings`, while
TQL updates that subtree in the original document and prints the whole document.

## What did not port cleanly

| jq feature | Why |
| --- | --- |
| `group_by`, `sort_by`, `unique_by` | No key-function variants; TQL `sort` does not take a projection |
| `to_entries`, `from_entries`, `with_entries` | No dynamic object-key construction |
| `@csv`, `@tsv`, `@base64` | No formatting filters |
| `\(.x)` string interpolation, `tostring`, `tonumber` | No general conversion/formatting built-ins |
| general `reduce`/`foreach` | Simple numeric folds work with `for`, arbitrary state folds do not |
| `try`/`catch`, `empty`, postfix `?`, `//` | TQL has no exception form; `??` is coalescing, not jq's alternative operator |
| user functions and modules | TQL scripts have no user-defined functions |
| `jq -s`, NDJSON, multi-document streams | The CLI processes one document per invocation |
| canonical key ordering | TQL preserves source order; reordering would require replacing containers |

## What this says about TQL

The best jq ports are **select/project**, **recursive findings**, **wildcard
updates**, **recursive field removal** and **simple folds**. They keep TQL's
advantage: comments, key order and surrounding layout survive.

Programs whose main job is to build a completely new JSON value are a poorer
fit. They can still be written, but TQL is a source-aware editor, not a
canonicalizing serializer.
