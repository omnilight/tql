# Architecture

TQL keeps the original source alongside a source-aware abstract syntax tree (AST).
Queries operate on decoded values; format backends plan edits against the original
bytes. The engine never serializes the entire document to make a local change.

```text
configuration bytes → format parser → source-aware tree
                                            │
TQL source → lexer and parser → program ──────┤
structured edit operations ──────────────────┘
                                            │
                              format-independent evaluation
                                            │
                              selected node addresses
                                            │
                              format-specific byte edits
                                            │
                           validate ranges → apply → reparse
                                            │
                             verify the full value tree
                                            │
                                commit or roll back
```

## Packages

| Package | Responsibility |
| --- | --- |
| `github.com/vasileknik76/tql` | Public API, format registry, execution, transactions, and script state |
| `edit` | Typed paths and operations, document diff and merge planning, and TQL export |
| `syntax` | Shared value tree, source ranges, format interface, and edit application |
| `formats/json`, `formats/toml`, `formats/yaml` | Handwritten parsers and format-specific edit planning |
| `internal/query` | TQL lexer, parser, and expression evaluation |
| `cmd/tql` | CLI, streams, file access, and checked file replacement |

The built-in parsers have no third-party runtime dependencies. A custom format
implements `syntax.Format` and registers with a `tql.Registry`; see the
[library guide](library.md).

## Source-aware trees

`syntax.Node` stores decoded values, ordered children, parent links, source ranges,
style information, and format-owned metadata. `syntax.Entry` also stores key and
key/value ranges. `Start` and `End` are half-open **UTF-8 byte offsets**, not rune
indexes.

Some containers have no single contiguous source range. A TOML table, for example,
can contain dotted assignments and headers in different parts of a file. Such
containers use `Start == -1`; their backend can return several edits for one
operation. Concrete scalar and inline values have normal contiguous ranges.

Parsers finish tree construction with `syntax.Link`. After linking, the tree is
immutable during evaluation. Format methods must not mutate the source, nodes,
entries, children, or metadata. They return `syntax.Edit` values in coordinates of
the same source snapshot. The engine applies an edit batch and obtains a fresh tree
by parsing the result.

## Byte preservation

“Char Perfect” means bytes outside the ranges required by the requested edit remain
unchanged. It has these practical boundaries:

- Reading and assigning a semantically equal value leave all source bytes unchanged.
- Replacing a scalar normally changes only its token. Replacing a YAML block scalar
  also removes its body and changes the indicator; its header comment is retained.
- Replacing a container includes its contents. Comments inside that subtree may be
  removed, and the backend chooses a representation for the new value. Change a
  child field when you want to retain the rest of the container's formatting.
- Inserting or deleting a member can require changes to separators, indentation,
  headers, or empty-container syntax. Existing keys are not reordered; keys in a
  newly encoded object use deterministic ordering, with YAML templates able to
  supply the field order for a new object.
- Comments outside the replaced or deleted subtree are retained. Backends also
  preserve trailing comments where possible. Deletions can leave blank lines or
  unused whitespace.

The result must remain representable in the target format. For example, assigning
`null` in TOML fails rather than silently converting it to another value. Details
and exceptions are documented in [format support](formats.md).

### Insertion style

Style inference applies to newly inserted text. Each backend reads lexical
evidence from the selected container and nearby values. A new collection can use
a neighboring collection of the same kind as a layout template; empty containers
can inherit conventions from their surroundings. Local evidence takes precedence
over a distant section of the document. When evidence is absent or a spelling
would be unsafe, the backend uses a deterministic, valid representation.

Depending on the format, inferred details include indentation, spaces around
separators, single-line or multiline layout, quote style, trailing commas, and line
endings. TOML and YAML strings are quoted whenever plain or literal spelling would
change their meaning. A string such as `"true"` must remain a string in YAML.
Comments, anchors, and existing scalar contents are never copied as style samples.
New object keys are sorted by default. YAML can follow a template's field order
and sorts additional fields; existing keys always retain their order.

This is best-effort local inference, not a configurable formatter. Mixed styles
and ambiguous alignment do not define a single correct result. It does not rewrite
existing entries or promise to retain a replaced container's internal formatting.
For appends, assign at an array's current length instead of replacing the array
with `+=`.

For example, given `name='api'` and `ports=[80,443]` on separate TOML lines,
`ports[2] = 8080; region = "eu"` produces:

```toml
name='api'
ports=[80,443,8080]
region='eu'
```

## Execution and transactions

Each modification statement snapshots its selected addresses and right-hand values.
Addresses contain decoded keys and array indexes rather than pointers into an old
tree. Array targets are processed from higher indexes to lower indexes. Overlapping
assignments are rejected; duplicate and nested deletions are combined.

Independent replacements from one statement are planned against the same snapshot
and applied as one batch. Structural operations and overlapping format edits use
sequential batches, reparsing between them so later edits use current ranges. This
matters for aliases: changing a child of a YAML alias may require replacing the
entire alias token.

After each batch, the engine compares the full parsed value tree with the expected
result. This catches unrequested semantic changes, including effects on YAML
aliases. An execution works on local state and commits to `Document` only when the
whole program succeeds. An error rolls back earlier statements and script output.
Source bytes and linked trees are shared immutably between edits; public `Document.Bytes()`
and `Result.Bytes` return independent copies.

Compiled programs contain the TQL AST, not mutable execution state. Each execution
creates its own lexical scopes and variables. `let` variables hold value snapshots;
`for` variables can refer to selected document nodes. Deleting an array element
adjusts remaining reference indexes, and deleted descendants become unavailable.
A loop therefore continues to refer to its original selected nodes after reparsing.

## Cost and ownership

With bounded tree depth, `k` independent replacements take approximately
`O(n + k log k)` plus expression evaluation, where `n` is document size. Sequential
structural edits and loop iterations can take `O(k × n)`. Lookup indexes keep wide
container access efficient, but execution is not streaming: memory includes source
bytes, trees, and query results. See [performance](performance.md) for benchmarks
and limitations.

Registries support concurrent access. A compiled program can be reused across
separate documents. A `Document` requires exclusive ownership when executing edits;
do not mutate and read it concurrently.

## Verification

Unit and fuzz tests cover parsers, query evaluation, exact output bytes, Unicode,
line endings, no-op edits, structural operations, and rollback. JSON tests use
`encoding/json` as an independent oracle; the production parser does not use it.
The [parser conformance tests](../conformance/README.md) run pinned upstream TOML,
YAML, and JSON corpora in ordinary Go tests and CI.

[File integration tests](integration-tests.md) build and run the real CLI against
isolated files, checking bytes, permissions, file identity, and failure behavior.
