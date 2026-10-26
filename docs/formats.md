# Format support

TQL provides handwritten parsers and source-preserving edit backends for JSON, TOML,
and YAML. Documents do not pass through an unmarshal/marshal round trip. The support
below describes the implemented scope; it is not a claim of certification against
complete external conformance suites.

## JSON

The parser targets [RFC 8259](https://www.rfc-editor.org/info/rfc8259/). It supports
all JSON value types, decimal numbers without `float64` rounding, string escapes,
and UTF-16 surrogate pairs. Input must be UTF-8, with nesting limited to 1,024
levels. Comments, trailing commas, and a byte order mark (BOM) are rejected.

Object keys must be unique after decoding: `"a"` and `"\u0061"` conflict. Unpaired
surrogates are rejected so decoded strings remain valid Unicode. These are
interoperability policies; other JSON implementations may accept such inputs.

Scalar replacement changes only the token. Deletion removes a member or element
and the necessary comma while leaving other whitespace in place; blank lines may
remain. Replacements use compact JSON. Insertions infer separator spacing,
indentation, line endings, and collection layout from their surroundings; nested
new collections can follow a nearby collection of the same kind. With no style
evidence, they use compact JSON. Deleting the root is an error.

## TOML

The parser targets [TOML 1.0](https://toml.io/en/v1.0.0). It supports implicit and
explicit tables, quoted and dotted keys, inline tables, arrays of tables, mixed and
multiline arrays, all four string forms, Unicode escapes, integer bases, floats,
dates and times, `inf`, and `nan`.

Integers are limited to signed 64-bit values; float overflow is rejected. Decimal
float spelling is retained for calculations rather than rounded to `float64`.
Dates are validated, but leap seconds are unsupported. Duplicate keys and
conflicting table definitions are errors. Value nesting and key paths are limited
to 256 levels. The root is always an object, and `null` is not representable at any
level. TOML 1.1 features are outside the target specification.

Inserting into a root or header table leaves unrelated sections untouched. Dotted
keys retain their individual source ranges. Deleting the last field retains an
empty parent object. Arrays of tables are exposed as arrays of objects: items can
be edited, appended, and deleted. Replacing one such item requires an object;
replacing the entire array can change its representation.

New entries infer assignment spacing, key and string quoting, and array layout
from nearby entries. Literal quotes are used only when the new text is legal in
that form. Object values use inline tables unless appended as an array-of-tables
item. Existing comments and scalar spellings remain untouched.

See the [TOML backend](../formats/toml/README.md) for edit behavior and the optional
independent differential test.

## YAML

The parser targets a configuration-oriented subset of
[YAML 1.2.2](https://yaml.org/spec/1.2.2/). It supports block and flow collections,
compact collections, comments, UTF-8 with an optional BOM, multiple line-ending
styles, core scalar resolution, quoted and plain strings, and literal/folded block
scalars with indentation and chomping indicators. Plain `yes`, `on`, and dates
remain strings, following YAML 1.2 rather than YAML 1.1 implicit typing.

Anchors and backward aliases resolve into a value tree. Editing inside an alias
materializes only that alias expression and preserves the anchor definition.
Editing an anchor that would change unselected aliases is rejected by the engine's
full-tree verification and rolled back. Recursive aliases, forward aliases,
duplicate anchor names, and excessive expansion are rejected.

This is not a complete YAML implementation. Complex keys, custom tags and schemas,
merge keys, and multiple-document streams are unsupported. See the
[YAML configuration profile](../formats/yaml/README.md) for the full supported and
rejected feature list, native tags, size limits, and alias rules.

Inserted keys, strings, and collections follow nearby quoting, indentation, and
block or flow layout where possible. Strings that could resolve as another type
are quoted. Replacements otherwise use JSON-compatible YAML flow syntax.
Empty block collections become
`{}` or `[]` after deleting their final member, preserving the collection type.
Deleting the root replaces it with `null`.

## Failed edits

A value must be representable in the selected format. Assigning TOML `null`,
replacing the TOML root with an array, or requesting an unsupported structural edit
returns an error. Parsing errors, backend errors, and unexpected semantic changes
roll back the complete execution. A successful parse means the document is accepted
by TQL's implemented profile, not that every feature of its format is supported.

For the exact byte-preservation contract, see [architecture](architecture.md#byte-preservation).
