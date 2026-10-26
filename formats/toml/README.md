# TOML backend

`toml.Format{}` implements `syntax.Format` with a handwritten
[TOML 1.0](https://toml.io/en/v1.0.0) parser and source-preserving edits. It has no
third-party runtime dependencies.

## Supported syntax

The parser supports quoted and dotted keys, implicit and explicit tables, arrays
of tables, inline tables, mixed and multiline arrays, all four string forms,
Unicode escapes, integer bases, floats, infinities and NaNs, and offset/local dates
and times.

Values retain source offsets. Header and dotted-key tables are synthetic object
nodes because their contents may occupy separate parts of the document. Arrays of
tables are synthetic arrays containing synthetic objects. Dates and times use
`syntax.Temporal`; numbers use `syntax.Number`. Integer spelling is normalized in
the value tree, while source bytes retain their original spelling until edited.

The parser rejects malformed UTF-8, duplicate definitions, invalid escapes and
dates, unsupported value tokens, integers outside signed 64-bit range, and float
overflow. Value nesting and dotted key paths are limited to 256 levels. Leap seconds
are unsupported. The target is TOML 1.0, not TOML 1.1; the backend has not been
certified against a complete external conformance suite.

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

## Differential testing

The optional deterministic harness compares this parser with Python's independent
standard-library `tomllib`. It requires Go and Python 3.11 or newer, introduces no
production dependency, and runs separately from `go test`:

```sh
python3 formats/toml/testdata/oracle/compare.py
```

Run the command from the repository root. With seed `541` and the default settings,
it compares acceptance and normalized values for 55,798 documents: representative
values and keys, 15,000 table/key declaration combinations, and 40,000 mutations.
The count includes repeated documents. The harness builds its Go helper in a
temporary directory and removes it afterward.

Two intentional numeric-policy differences are reported separately: Python accepts
integers outside signed 64-bit range and converts float exponent overflow to
infinity; this backend rejects both. The harness recognizes a policy difference
only when the Go error and Python value confirm it. Any unexpected acceptance or
semantic difference causes exit status 1.

For a smaller run, optionally saving unexpected cases:

```sh
python3 formats/toml/testdata/oracle/compare.py --cases 1000 --combinations 500 \
  --save-mismatches /tmp/tql-toml-mismatches.json
```

`--cases` controls mutations; representative cases are always included. This is a
targeted differential check, not a complete conformance suite. Discovered failures
should also become focused Go regression tests.
