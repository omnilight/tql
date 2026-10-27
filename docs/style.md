# Style for generated values

TQL changes selected source ranges. Style is a **generation policy**, rather than
an instruction to format the whole document. Existing containers keep their layout
when a member or item is added. Equal assignments remain byte-for-byte no-ops.
Whole-container replacements still have the replacement behavior documented in
[architecture](architecture.md#byte-preservation); use member assignment or append
for style-aware additions. An empty YAML document also uses this generation policy
when its initial value tree is created.

## CLI and Go API

The default is `auto`, independently for every setting:

```sh
# Follow the document: [obj2] in a table-based file, {v = 1} in an inline file.
tql 'obj2.v = 1' config.toml

# Choose collections together, or override one kind independently.
tql --style block 'obj2.v = 1' config.toml
tql --object-style flow --array-style block 'new = {items: [1, 2]}' config.yaml

# Fine-grained scalar and whitespace preferences.
tql --string-style single --key-style plain 'region = "eu"' config.yaml
tql --number-style hex 'permissions = 493' config.toml
tql --array-style block --indent 4 --line-ending lf 'ports = [80, 443]' config.toml
```

`--style auto|block|flow` is shorthand for the two collection settings. A supplied
`--object-style` or `--array-style` overrides the shorthand regardless of argument
order, including an explicit `auto`. All flags precede the query.

```go
doc, err := tql.Parse(source, "toml", tql.WithStyle(tql.Style{
    Objects: "block",
    Arrays: "auto",
    Strings: "single",
    Keys: "plain",
    Numbers: "auto",
    Indent: "    ",
    LineEnding: "lf",
}))
```

`Parse`, `ParseWith`, `Registry.Parse`, and `Eval` accept these options. The policy
belongs to the document and is used by `Exec`, `Execute`, and structured `Apply`,
including patches created by `edit.Diff`/`edit.Merge`. Failed transactions still
roll back all changes. Registries and other documents are unaffected. Repeated
`WithStyle` options replace the whole policy; empty fields mean `auto`.

Format backends can be configured directly through `Format.WithStyle(syntax.Style)`.
It returns an independent `syntax.Format`. The existing `syntax.Format` interface
is unchanged; custom backends may implement the optional `syntax.StyledFormat`
interface. A non-auto policy on a backend without that capability returns an error.
JSON retains its existing source-derived insertion behavior and rejects explicit
policies. Invalid or format-incompatible settings return errors before execution.

## Choices by value type

| Kind / setting | YAML | TOML |
| --- | --- | --- |
| Objects `block` | Indented mapping | `[table]` sections, recursively |
| Objects `flow` | `{key: value}` | `{ key = value }` inline tables |
| Objects `dotted` | Unsupported | `object.child.key = value` assignments |
| Arrays `flow` | Bracket sequence | Bracket array on one line, apart from multiline child values |
| Arrays `block` | Dash sequence | `[[table]]` for nonempty arrays containing only objects; multiline brackets otherwise |
| Strings `plain` | Unquoted when safe | Unsupported |
| Strings `single` | Single quotes with doubled apostrophes | Single-line literal quotes |
| Strings `double` | Double quotes and escapes | Single-line basic quotes and escapes |
| Strings `literal` | `|` block scalar | Triple single quotes |
| Strings `folded` | `>` block scalar | Unsupported |
| Strings `multiline` | Unsupported | Triple double quotes |
| Keys `plain`, `single`, `double` | Safe plain / quoted key | Bare / literal / basic quoted key |
| Numbers `decimal`, `hex`, `octal` | Exact integer spelling | Exact integer spelling |
| Numbers `binary` | Unsupported in YAML 1.2 | `0b…` |
| Booleans | Infer lower/title/upper case | Lowercase, required by TOML |
| Null | Infer `null`, `Null`, `NULL`, `~` | Not representable |
| Date/time | A plain date is a string under the YAML core schema | Preserve the `Temporal` value's exact spelling |
| Indent | Spaces only | Spaces or tabs |
| Line ending | `lf`, `crlf` for new text | `lf`, `crlf` for new text |

`auto` is available for every enum above. `Style.Indent == ""` means inference;
otherwise it contains 1–16 whitespace characters. CLI `--indent` accepts `auto`, a
number of spaces from 1 to 16, or `tab` for TOML.

Preferences never change values to make a spelling fit. Unsafe plain text is
quoted; strings containing characters forbidden in literal quotes use escaped
double quotes. Block-string emission checks indentation, folding and chomping
against the parser. A folded representation that would alter the value falls
back to literal style, then to double quotes if necessary. Newline counts and
leading spaces are data and must survive. No line-width-based word wrapping is
performed.

Numeric policy changes integral decimal values only. `auto` infers an existing
integer base; negative values use decimal when the preferred base would be illegal.
Floats and decimal exponents keep their exact representation: TQL does not infer
precision, round values, add digit grouping, or impose numeric widths. TOML's
signed 64-bit integer limit still applies.

## How auto chooses

1. Respect the grammar and existing destination container. YAML flow descendants
   must remain flow; objects inserted inside a TOML inline table remain inline.
   Appending to an existing bracket array or array of tables keeps that array's
   representation even if the policy prefers another one.
2. For a new value, inspect same-kind examples among the destination's children.
   Use the majority representation in the closest scope containing evidence,
   with ties resolved by the last example. Search enclosing scopes only when
   local evidence is absent. Arrays of objects and other arrays are separate
   comparison groups; a list of ports should not dictate a list of servers.
3. Use the selected example for collection layout or scalar spelling, and the
   existing local whitespace logic for separators, alignment, quotes and newlines.
   Explicit settings override these preferences. Comments, anchors, scalar
   contents and names are never copied into generated text.
4. With no evidence, use deterministic defaults: block YAML collections; TOML
   table objects, array tables for object arrays, brackets for other arrays;
   safe quoted strings, decimal numbers, two-space indentation and LF newlines.
   Multiline inline-table templates retain their line layout.
   New TOML object arrays can also inherit an inline-object convention when no
   existing object array provides a template.

In TOML a nested header must stay within the selected `[[table]]` element. The
editor inserts before the next element, rather than appending a child at EOF and
accidentally attaching it to a later element. Section bodies emit assignments
before descendant headers so ownership remains correct.

Empty YAML collections require `{}` or `[]`, even under `block`. Empty TOML arrays
require `[]`, because no `[[table]]` header can describe zero items. Mixed TOML
arrays use brackets. Empty dotted objects use inline `{}`. Grammar constraints
also override conflicting per-kind preferences: a YAML flow object cannot contain
a block array. Existing untouched source retains its line endings even when new
lines explicitly use a different ending.

Alias edits in YAML materialize the selected alias; inserts and explicit policies
can style that materialized flow value. Automatic alias replacements retain the
existing canonical replacement behavior. Anchor definitions remain untouched and
full semantic verification guards aliases, types and neighboring values.

## Existing approaches and configuration decision

The format inventories follow [YAML 1.2.2](https://yaml.org/spec/1.2.2/) and
[TOML 1.1](https://toml.io/en/v1.1.0). Structure, scalar spelling and whitespace
are separate concerns, so one global `inline` switch cannot express them all.

[EditorConfig](https://editorconfig.org/index) standardizes indentation, line
endings, encoding, trailing whitespace and final newlines. It has no standard
property for YAML flow/block or TOML table/inline/dotted structure. Consequently,
reading `.editorconfig` alone would not solve the original problem.

[Prettier](https://prettier.io/docs/configuration#editorconfig) translates a subset
of EditorConfig properties into formatter settings; its own configuration can
override them. That separation is useful, but whole-file normalization would
conflict with TQL's targeted-edit contract. TQL also avoids applying trimming,
encoding conversion or final-newline normalization to untouched text.

[ruamel.yaml](https://yaml.dev/doc/ruamel.yaml/detail/) retains presentation
information for round trips and exposes detailed indentation controls.
[TOML Kit](https://tomlkit.readthedocs.io/en/stable/api/) distinguishes table and
inline-table syntax objects. These approaches support retaining syntax metadata
and choosing an explicit representation when constructing new nodes. TQL follows
that principle while keeping ordinary Go maps, slices and scalars in its edit API.

The implemented boundary is an explicit, document-local `Style` value plus CLI
flags. Neither the library nor CLI searches the filesystem for formatter settings.
There is no new `.tqlrc` format and no nonstandard `tql_*` EditorConfig properties.
Callers with existing project configuration can translate `indent_size` and
`end_of_line` into `Style.Indent` and `Style.LineEnding`, then set structural
preferences through the same API. This keeps stdin and in-memory use reproducible,
maintains the dependency-free library, and leaves configuration discovery separate
from syntax generation.
