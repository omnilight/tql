# Structured editing from Go

Package `edit` builds source-preserving changes from Go paths and values. A patch
can be assembled manually, produced by comparing two documents, or planned by a
merge. All three cases use the same operations and are applied atomically.

```go
import (
    "github.com/vasileknik76/tql"
    "github.com/vasileknik76/tql/edit"
)

doc, err := tql.Parse(source, "yaml")
if err != nil {
    return err
}

patch := edit.Patch{
    edit.Set(edit.Key("server", "port"), 9090),
    edit.Set(edit.Key("users").Index(0).Key("enabled"), true),
    edit.Del(edit.Key("legacy")),
}

result, err := doc.Apply(patch...)
```

`Set`, `Del`, `Diff`, and `Merge` only construct commands. `Document.Apply` is the
operation that changes a document.

## Paths and operations

A path contains literal object keys and nonnegative array indexes:

```go
edit.Root()                              // document root
edit.Key("server", "port")              // nested keys
edit.Key("a.b")                         // one key containing a dot
edit.Key("users").Index(2).Key("name")   // array element field
edit.Root().Index(0)                    // root array element
```

The zero `Path`, `Root()`, and `Key()` all address the root. Extending a path
returns a new value and does not change the original. `Path.String()` produces a
canonical TQL address with quoted keys; for example, `Key("a.b").Index(0)` becomes
`.["a.b"][0]`.

Paths intentionally do not parse dotted strings. This keeps `"a.b"` unambiguous.
Filters, slices, wildcards, expressions, and negative indexes belong to the TQL
language and can still be used through `Document.Exec`.

There are two operation kinds:

```go
op := edit.Set(path, value)
op := edit.Del(path)
```

`Set` assigns a value. It creates missing object parents, replaces `null` parents
with the required containers, and appends to an array when the index equals its
length. Sparse arrays and traversal through a non-null scalar return errors.
Setting the root replaces the document value when the target format supports it.

`Del` removes an object field or array element. A missing target is a no-op;
deleting the document root is an error. An explicit `null` remains distinct from
a missing field.

Constructors capture detached value and path snapshots. Later changes to the
original maps or slices do not affect an operation. `Kind()`, `Path()`, and
`Value()` expose detached command data when a caller needs to inspect a patch.

Operations execute in slice order. Every path resolves against the result of the
previous operation, so deleting array index zero changes the indexes seen by later
commands. Repeated and overlapping paths are allowed.

## Atomic application

```go
result, err := doc.Apply(patch...)
```

`Apply` uses the same format backends and source-aware mutation engine as TQL
scripts. It returns the existing `tql.Result` type:

- `Bytes` contains detached final source.
- `Changed` reports a byte-level change.
- `Values` is empty because structured operations do not produce query results.

An empty patch succeeds without changing bytes. If any operation has an invalid
path or value, cannot be represented by the document format, or fails while being
applied, the whole call returns an error and leaves the document unchanged.

Equal assignments are no-ops and retain the original spelling. Other preservation
rules are the same as for TQL assignments: scalar edits normally change one token,
while replacing a whole container can replace formatting and comments inside that
container. See the [source-preservation contract](architecture.md#byte-preservation).

## Diff

`Diff` returns a patch that makes the first document semantically equal to the
second:

```go
patch, err := edit.Diff(current, desired)
if err != nil {
    return err
}

patch = append(patch, edit.Set(edit.Key("managed"), true))
result, err := current.Apply(patch...)
```

Both arguments are `*tql.Document`. Parse bytes with `tql.Parse` before comparing
them. The documents may use different formats; the first document's format checks
whether target values are representable when the patch is applied.

`Diff` modifies neither input and does not retain document references. Its output
is deterministic:

- Objects delete absent keys and recursively update existing keys in lexical order.
- Arrays compare their common prefix by index, delete a surplus tail from the end,
  and append a new tail in increasing order.
- Different value kinds and unequal scalars produce one `Set`.
- Semantically equal values produce nothing, including numbers such as `1` and
  `1.0`.

The result is a structural diff, not a minimal edit script. Array positions define
identity; moves and application-specific identifiers are not inferred. Applying a
patch later also does not verify that its starting document still equals the
original input, so recompute it when the source may have changed.

`Diff` transfers semantic values, not presentation. Applying a diff preserves the
first document's unaffected comments, order, whitespace, quotes, and line endings;
it does not copy formatting from the second document.

## Merge

`Merge` overlays the second document on the first and returns the changes to apply
to the first:

```go
patch, err := edit.Merge(existing, template,
    edit.WithArrays(edit.UnionArrays),
    edit.WithReplace(edit.Key("provider", "options")),
)
if err != nil {
    return err
}
result, err := existing.Apply(patch...)
```

Objects merge recursively. Keys present only in the base survive. For scalars,
different kinds, and explicit `null`, the overlay wins.

Arrays support three policies:

| Mode | Base `[1, 2]`, overlay `[2, 3]` |
| --- | --- |
| `ReplaceArrays` (default) | `[2, 3]` |
| `AppendArrays` | `[1, 2, 2, 3]` |
| `UnionArrays` | `[1, 2, 3]` |

`UnionArrays` preserves all base elements and appends overlay elements that are
not semantically equal to any element already present. Equality is recursive and
numeric, so `1`, `1.0`, and `1e0` are equal. Objects inside arrays are compared as
whole values rather than merged by a field.

`WithReplace(paths...)` makes the selected overlay subtrees replace the matching
base subtrees instead of merging their object keys. Paths must exist in the
overlay and may traverse object keys only; the selected value itself may be an
array or scalar. `Root()` replaces the whole base. Multiple options accumulate,
and a parent path subsumes its children.

Merge still produces a structural diff between the base and the desired value.
Thus `WithReplace` removes base-only keys in the selected subtree while retaining
the formatting of unchanged descendants. Use an explicit `Set` when the whole
container should be replaced as one operation.

`Merge` modifies neither input. Default and union merges are idempotent when
recomputed after application. Append merge deliberately is not.

## Values

`Set` accepts TQL's semantic values:

- `nil`, `bool`, and UTF-8 `string`
- `map[string]any` and `[]any`
- `tql.Number` and `tql.Temporal`
- built-in integer and finite floating-point types

Integers become exact decimal `tql.Number` values. Floats use their shortest
round-trip decimal representation; use `tql.Number` when decimal spelling or
precision is application data. Nil typed maps and slices become empty containers,
while untyped `nil` is `null`.

Other maps, slices, structs, pointers, custom marshalers, `json.Number`, invalid
UTF-8, cyclic values, and invalid number or temporal spellings are rejected. TQL
does not marshal application structs implicitly; convert them to the documented
semantic types first.

The target format must represent the value without changing its type. TOML, for
example, rejects `null`; JSON and YAML reject `tql.Temporal` and non-finite numbers.

## Export a patch as TQL

```go
text, err := patch.TQL()
if err != nil {
    return err
}

result, err := doc.Exec(text)
```

`TQL()` returns deterministic executable source, with one command per line. Paths
and values are escaped, so literal keys and strings cannot inject TQL syntax.

```tql
.["server"]["port"] = 9090;
.["users"][0]["enabled"] = true;
del(.["legacy"]);
```

An empty patch exports `if true {}\n`, because an empty string is not a valid TQL
program. The exporter validates generated source with `tql.Compile`.

TQL has no literals for typed TOML dates or non-finite numbers. A patch containing
one of those values can still be applied directly, but `TQL()` returns an error and
no partial text rather than converting the value to a string.
