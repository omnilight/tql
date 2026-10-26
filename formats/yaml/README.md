# YAML graph and query APIs

The package provides a handwritten, source-aware parser for UTF-8
[YAML 1.2.2](https://yaml.org/spec/1.2.2/) syntax, without third-party runtime
dependencies. `ParseStream` exposes complete syntax graphs; `Format{}` projects a
single document into TQL's value-tree model and plans source-preserving edits.

## Syntax graphs and streams

`ParseStream` accepts document streams, block and flow collections, explicit and
complex mapping keys, empty nodes, directives, native and custom tags, anchors,
and aliases. LF, CRLF, and CR line endings and an initial UTF-8 BOM are supported.

```go
stream, err := yaml.ParseStream([]byte("---\n? [a, b]\n: !label value\n---\n&node [*node]\n"))
if err != nil {
    return err
}
firstKey := stream.Documents[0].Root.Content[0] // SequenceNode: [a, b]
alias := stream.Documents[1].Root.Content[0]
_ = firstKey
_ = alias.Alias == stream.Documents[1].Root // true
```

A stream owns a copy of its input, and `Bytes()` returns another independent copy.
Errors return no partial stream. An empty or comment-only input has no documents;
explicit empty documents have a null scalar root. Each document records its source
span, version directive, and explicit start/end markers.

Nodes expose their kind, decoded scalar text, resolved tag identifier, anchor,
source span, and style. Sequence `Content` contains its elements; mapping `Content`
alternates key and value nodes. Keys retain their own types and structure. An
`AliasNode` holds the alias name in `Value` and its captured target in `Alias`.
Targets preserve identity, including recursive aliases and lexical anchor
rebinding. Traversal should follow `Content`; following `Alias` edges can revisit
nodes.

This is a syntax graph, not a schema-specific loaded value. Implicit scalar tags
use YAML 1.2 core resolution, while scalar text retains its spelling, including
tagged values that a particular schema cannot construct.
Unknown tags and ordered duplicate mapping entries remain visible. No complex key
is converted to a string and no custom tag is discarded. Applications can apply
their own schema after parsing.

## Query values

The `Format` backend shares the same parser. It accepts one representable document;
empty input retains TQL's null-document convention. Plain scalar resolution uses
YAML 1.2 core nulls, booleans, and finite decimal numbers, plus unsigned octal `0o`
and hexadecimal `0x` integers. Integers retain arbitrary precision. Plain `yes`,
`no`, `on`, dates, and times remain strings.

Mapping keys are exposed as strings: nonempty plain primitive key spelling is
retained, and quoted string keys are decoded. Explicit string keys and aliases to
string keys are supported. Duplicate or colliding projected keys are errors.

Native `!!str`, `!!int`, `!!float`, `!!bool`, `!!null`, `!!seq`, and `!!map` tags
are constructed according to their types, including the verbatim
`!<tag:yaml.org,2002:...>` form. Replacing a value retains a compatible native tag
and removes an incompatible one when changing its type.

Queries reject multiple documents, complex or empty null keys, custom tags, merge
keys, non-finite numbers, recursive aliases, and invalid native typed values.
These graphs remain available through `ParseStream`; the query backend does not
silently flatten or drop them. Unknown and forward aliases are parsing errors.
Nesting is limited to 256 levels. Query alias expansion is limited to 100,000 nodes;
the graph API retains alias edges instead of expanding them.

## Edit behavior

The backend plans byte-range edits against the original UTF-8 source. It does not
reserialize the document. Bytes outside the necessary edit ranges retain their
keys, ordering, whitespace, comments, line endings, and Unicode spelling.

Scalar replacement changes the selected value span. String replacement retains
single-quoted style when possible; replacements otherwise use JSON-compatible YAML
flow syntax. Insertions infer key and string quoting, separator spacing, collection
layout, indentation, and line endings from nearby entries. Plain strings are used
only when they keep their type and contents; ambiguous text is quoted. Empty
containers inherit useful surrounding conventions, with valid defaults when no
style can be inferred. Comments, anchors, and tags are not duplicated into new
entries. Array insertion supports
append, using an empty key, `"-"`, or the current array length.

A new mapping can follow the field order of its template, useful when appending
similar objects to a sequence. Fields absent from the template are sorted. Alias
materialization uses the existing flow representation when no lexical template
is available.

Deletion removes the entry's syntax and necessary separators. A trailing inline
comment remains as a standalone comment where possible. Comments inside a deleted
or replaced subtree may be removed. An emptied block collection becomes `{}` or
`[]` to preserve its type. Replacing a block string retains its header comment and
uses quoted YAML for the new value. Appending to a file without a final newline
preserves that convention. Deleting the root replaces it with `null`.

Neither source bytes nor the linked tree are mutated by format methods. Apply an
edit batch with `syntax.Apply`, then reparse before planning the next batch. The TQL
engine does this automatically and verifies the full semantic result. See the
[library guide](../../docs/library.md) for using formats and registering extensions.

## Anchors and aliases

Editing inside an alias materializes only that alias token. For example:

```yaml
defaults: &defaults {name: original, enabled: true}
copy: *defaults
```

Applying `copy.name = "updated"` replaces `*defaults` with a flow mapping containing
the updated name and the original `enabled` value. The anchor definition stays
unchanged.

Editing the anchor definition changes how its remaining aliases resolve when the
document is reparsed. TQL compares the entire result with the requested value tree
and rejects unrequested alias changes, rolling back the execution. Removing an
anchor that is still referenced produces an invalid document and is also rejected.

Callers using backend methods directly must perform their own reparse and semantic
verification; `syntax.Apply` validates byte ranges, not YAML or alias semantics.

## Conformance checks

The optional harness compares `ParseStream` with the
[official YAML Test Suite](https://github.com/yaml/yaml-test-suite). Download a
published data export, such as `data-2022-01-17`, separately, then run from the
repository root:

```sh
make yaml-suite YAML_TEST_SUITE=/path/to/yaml-test-suite-data
```

The harness builds a temporary Go helper and reads every `in.yaml`, including
numbered subtests. Valid inputs must match the expected event stream: document
markers, collection styles, decoded text, tags, anchors, and aliases. Where
`in.json` is available, decoded values and document counts are compared as well,
without rounding numbers. Invalid inputs must return an error without a partial
stream. Crashes, timeouts, missing references, and an empty corpus fail the run.

Only Go and Python are required; the harness does not download the suite or add
production dependencies. Use `--save-results` on
`formats/yaml/testdata/suite/compare.py` to retain individual results. It tests
syntax graphs, not arbitrary graph editing or every possible YAML schema.

`make fuzz-yaml-graph` checks graph ownership, spans, mapping structure, and alias
targets; `make fuzz` includes it alongside query-backend and edit fuzz checks.
