# Go API

Import `github.com/vasileknik76/tql`. The module requires Go 1.23 or later and has
no third-party dependencies.

## Parse and execute

```go
package main

import (
    "fmt"
    "log"
    "os"

    "github.com/vasileknik76/tql"
)

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}

func run() error {
    source, err := os.ReadFile("config.toml")
    if err != nil {
        return err
    }
    doc, err := tql.Parse(source, "toml")
    if err != nil {
        return err
    }
    values, err := doc.Query(`server.port`)
    if err != nil {
        return err
    }
    fmt.Println(values)

    result, err := doc.Exec(`server.port = 9090; server.port`)
    if err != nil {
        return err // doc still contains its original bytes
    }
    fmt.Print(string(result.Bytes))
    return nil
}
```

`Parse` copies its input. `Bytes` returns a copy of the source, and `Value` returns
an independent semantic tree. Modifying those results does not change the document.
`Parse` requires an explicit format name; `DetectFormat(filename)` recognizes
filename extensions without reading file contents.

`Exec(text)` compiles and executes a program. `Execute(program)` runs a previously
compiled program. Both commit changes only when the **entire** program succeeds.
An error returns no partial read results and leaves the document unchanged.

`Result` contains:

| Field | Meaning |
| --- | --- |
| `Bytes []byte` | Final source bytes, independent of the document's storage |
| `Values []any` | Results produced by read expressions; mutations produce none |
| `Changed bool` | Whether the final source differs byte-for-byte from the source before execution |

`Query(text)` returns only read values and rejects programs containing assignments
or deletions. This check is conservative: assignments to local variables also
require `Exec`/`Execute`, even if they leave `Result.Changed` false.
A `let` declaration alone is not classified as a mutation.

For a single operation, `tql.Eval(source, "toml", "server.port = 9090")` combines
parsing and execution.

## Reuse a compiled program

The following fragment assumes `inputs` is a `[][]byte` and the enclosing function
returns an error:

```go
program, err := tql.Compile(`items[].price |= . * 2`)
if err != nil {
    return err
}
for _, source := range inputs {
    doc, err := tql.Parse(source, "json")
    if err != nil {
        return err
    }
    result, err := doc.Execute(program)
    if err != nil {
        return err
    }
    fmt.Print(string(result.Bytes))
}
```

A `Program` is immutable and safe to reuse concurrently with different documents.
`Program.Mutates()` reports whether it contains any assignment or deletion,
including local variable changes. A `Document` is not synchronized: use separate
documents or external locking for concurrent access. Registries are safe for
concurrent registration and lookup.

Scripts use the same API:

```go
program, err := tql.Compile(`
    let $delta = 10;
    for $server in servers[] {
        if $server.enabled { $server.port += $delta; }
    }
`)
```

Check the compilation error, then call `doc.Execute(program)`. Each execution has
its own variable environment. An error in any iteration rolls back the whole
execution. See [scripting semantics](scripts.md).

## Value types

| TQL value | Go type |
| --- | --- |
| Object | `map[string]any` |
| Array | `[]any` |
| String | `string` |
| Number | `tql.Number`, an alias of `syntax.Number` |
| Boolean | `bool` |
| Null | `nil` |
| TOML date/time | `tql.Temporal`, an alias of `syntax.Temporal` |

`Number` is a named string, such as `tql.Number("9007199254740993")`. Its `String`
and `MarshalJSON` methods preserve exact finite numbers when passed to
`encoding/json`. TOML `inf` and `nan` values marshal as strings; TQL arithmetic on
them returns an error. Convert explicitly when an application needs another
numeric type, for example `strconv.ParseInt(string(number), 10, 64)`.

TOML/YAML numeric spellings are normalized in semantic values (`0xFF` becomes
`255`), while source bytes keep their spelling until edited. TOML date/time is a
separate semantic type that retains its textual spelling, rather than `time.Time`.

## Add a format

A format implements `syntax.Format`, also exported as `tql.Format`:

```go
type Format interface {
    Parse(source []byte) (*syntax.Node, error)
    Replace(source []byte, root, target *syntax.Node, value any) ([]syntax.Edit, error)
    Insert(source []byte, root, parent *syntax.Node, key string, value any) ([]syntax.Edit, error)
    Delete(source []byte, root, target *syntax.Node) ([]syntax.Edit, error)
}
```

Use an implementation directly without changing a registry:

```go
import tomlformat "github.com/vasileknik76/tql/formats/toml"

// Inside a function; check err before using doc.
doc, err := tql.ParseWith(source, tomlformat.Format{})
```

Or use an isolated registry:

```go
registry := tql.NewRegistry() // starts empty
if err := registry.Register("myconfig", myFormat); err != nil {
    return err
}
doc, err := registry.Parse(source, "myconfig")
```

`RegisterFormat` adds an implementation to the default JSON/TOML/YAML registry.
Duplicate names return an error; registration names are trimmed and lowercased.
A zero-value `Registry` also supports registration. Format implementations must
be safe for concurrent calls.

### Source-aware trees and edits

`Parse` returns nodes with exact half-open byte ranges into the UTF-8 source.
`syntax.Link` fills parent links and lookup indexes; the library calls it after
parsing. Synthetic nodes such as implicit TOML tables may have `Start == -1`.
`Style` and `Meta` hold format-specific data. Edit methods must not mutate the
input buffer or tree.

`Insert` receives a missing object key or an array index equal to its length.
The built-in formats also accept `"-"` for an append. Return an error when a value
cannot be represented: TOML, for example, cannot store `null` or a scalar root.

The library validates edit ranges, applies edits with `syntax.Apply`, reparses the
result, and compares the entire semantic tree with the expected change. An edit
that loses an unrelated field or inserts into the wrong table fails and rolls
back, even if the output is syntactically valid.

Use a format's `Parse` method directly to inspect its `syntax.Node` tree. Formatting
lives in the source buffer and node ranges. After `syntax.Link`, reading a tree
does not populate caches. Do not mutate it during evaluation; after constructing
or rebuilding a tree manually, call `syntax.Link` again while holding exclusive
ownership. See [architecture](architecture.md) and [performance](performance.md)
for the full editing model.
