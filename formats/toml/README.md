# TOML backend

`toml.Format{}` implements `syntax.Format` with a handwritten
[TOML 1.1](https://toml.io/en/v1.1.0) parser and source-preserving edits. It has no
third-party runtime dependencies.

## Supported syntax

The parser supports quoted and dotted keys, implicit and explicit tables, arrays
of tables, inline tables, mixed and multiline arrays, all four string forms,
Unicode escapes, integer bases, floats, infinities and NaNs, and offset/local dates
and times. TOML 1.1 adds `\e` and `\xHH` escapes in basic strings and quoted keys,
optional seconds in times and date-times, and newlines, comments, and trailing
commas in inline tables. For example:

```toml
name = "Jos\xE9"
meeting = 1979-05-27T07:32Z
settings = {
  enabled = true,
}
```

Values retain source offsets. Header and dotted-key tables are synthetic object
nodes because their contents may occupy separate parts of the document. Arrays of
tables are synthetic arrays containing synthetic objects. Dates and times use
`syntax.Temporal`; numbers use `syntax.Number`. Integer spelling is normalized in
the value tree, while source bytes retain their original spelling until edited.
Temporal values retain their input spelling, including omitted seconds.

The parser rejects malformed UTF-8, duplicate definitions, invalid escapes and
dates, unsupported value tokens, integers outside signed 64-bit range, and float
overflow. Value nesting and dotted key paths are limited to 256 levels. Leap seconds
are unsupported.

## Edit behavior

Scalar, array, and inline-table replacements affect only the value span. Deletion
removes the selected assignment or value and any necessary comma. Unrelated
assignments, headers, whitespace, and comments remain unchanged. A removed key's
trailing comment can remain as a standalone comment. Removing the final array-table
item produces `key = []`.

Insertions infer local assignment spacing, key and string quotes, array separators,
indentation, and line endings. Nearby values of the same kind provide templates
for new collections. A literal string falls back to basic quotes when its contents
cannot be represented safely. With no useful evidence, the backend uses its
deterministic defaults. New object values use inline tables.
New keys in a header table are inserted into that section, while keys in implicit
dotted tables use dotted spelling. Array appends accept `"-"` or the current array
length as the insertion key. Nested arrays of tables are appended in the section
belonging to the selected parent item.

Replacing or deleting a header table changes its assignments and headers while
retaining comments. Replacing an individual array-table item requires an object
and retains its header; replace the whole array to change its representation.
Root replacement requires an object. TOML has no `null`: inserting or replacing a
null value fails, including a null nested in another value.

Format methods return byte-range edits without mutating the source or linked tree.
Apply a batch with `syntax.Apply`, then parse again before planning another batch.
The TQL engine performs this process and verifies the full resulting value tree;
see [architecture](../../docs/architecture.md).

## Conformance checks

All 681 TOML 1.1 parser cases from `toml-lang/toml-test` v2.2.0 are pinned in
`conformance/testdata/toml` and run in `go test ./...` and CI. Valid inputs are
compared with the upstream typed JSON values, including integer/float distinctions,
negative zero, special floats, and local/offset temporal types. Invalid inputs must
return an error without a partial tree. No downloads or Python are needed.
See [parser conformance](../../conformance/README.md) for details.
