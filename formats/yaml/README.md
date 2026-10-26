# YAML configuration profile

`yaml.Format{}` implements `syntax.Format` with a handwritten, source-aware parser
for a configuration-oriented subset of
[YAML 1.2.2](https://yaml.org/spec/1.2.2/). It is not a complete YAML implementation.

## Supported syntax

- UTF-8 with an optional BOM; LF, CRLF, and CR line endings; comments and blank lines.
- Block mappings and sequences, compact mapping entries in sequences, nested and
  indentationless sequences, compact nested sequences, and flow mappings and
  sequences, including multiline flow collections and trailing commas.
- Plain scalars with multiline folding, single-quoted strings with doubled quotes,
  double-quoted strings with YAML and Unicode escapes, multiline quoted folding
  and escaped line breaks, and literal/folded block strings with chomping (`+`, `-`)
  and explicit indentation indicators.
- YAML 1.2 core nulls, booleans, and finite decimal numbers; octal `0o` and hexadecimal
  `0x` integers. Integers retain arbitrary precision. Plain `yes`, `no`, `on`, dates,
  and times remain strings. Mapping keys are exposed as strings: plain key spelling
  is retained, and quoted keys are decoded.
- One document, optional `---` and `...` markers, and an optional `%YAML 1.2` directive.
- Anchors and backward aliases for scalar, sequence, and mapping values. Aliases
  resolve to independent semantic subtrees. Recursive aliases, forward aliases,
  duplicate anchors, and expansion beyond 100,000 alias nodes are rejected.
- Native `!!str`, `!!int`, `!!float`, `!!bool`, `!!null`, `!!seq`, and `!!map` tags,
  including the verbatim `!<tag:yaml.org,2002:...>` form. Replacing a value retains
  a compatible tag and removes an incompatible tag when changing its type.

## Unsupported syntax

Custom tags, `%TAG` directives, merge keys, complex or explicit keys, empty
unquoted keys, duplicate keys, implicit mapping entries inside flow sequences,
non-finite numbers, and multiple-document streams are rejected. Nesting is limited
to 256 levels. Alias nodes cannot themselves carry anchor or tag properties.

Unsupported constructs return errors. Acceptance by this parser should not be used
as a claim of full YAML 1.2 compatibility.

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
