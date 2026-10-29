# TQL language reference

TQL operates on objects, arrays, strings, numbers, booleans, `null`, and TOML
date/time values. Keys are case-sensitive. Pass a query as one CLI argument,
usually in single shell quotes, or load a program with `--query-file`.

For input parameters, variables, conditions, and loops, see the
[scripting guide](scripts.md).

## Paths and streams

| Syntax | Meaning |
| --- | --- |
| `.` | Current input; initially the document |
| `$` | Document root, including inside a filter or pipe |
| `foo.bar`, `.foo.bar` | Object field |
| `.["a.b"]` | A key containing punctuation or other special characters |
| `foo[0]`, `foo[-1]` | Array index; negative indexes count from the end |
| `foo[]`, `foo[*]`, `foo.*` | All array elements or object values |
| `foo[1:4]`, `foo[:2]`, `foo[-2:]` | Half-open array slice |
| `foo[name == "alice"]` | Array elements or object values matching a predicate |
| `foo[] \| .name` | Evaluate the right side for each result of the left side |

Wildcards, filters, and slices produce a **stream of nodes**, not a new array.
Use brackets in expression position to collect a stream:

```tql
[users[].name]
[users[] | select(.active) | .name]
```

Reading a missing key or index produces an empty stream. Where exactly one value
is required, such as the right side of `=`, an empty stream becomes `null` and
multiple values cause an error. Reading a key or index from `null` also produces
an empty stream. Accessing a field of a number or indexing a string is an error.

Use a leading dot for fields named after zero-argument functions: `.length` reads
a field, while bare `length` calls the function. Use brackets for keys containing
hyphens or spaces, numeric keys, and reserved words. An identifier starts with a
Unicode letter or `_`, followed by letters, digits, or `_`.

## Values and operators

```tql
42
-1.25e3
"string\nvalue"
'single quoted string'
true
false
null
[1,2,3]
{host: "localhost", "retry-count": 3}
{names: [users[].name], count: length(users)}
```

Strings use JSON escapes; single-quoted strings additionally accept `\'`.
Unicode escapes support surrogate pairs. Numbers use JSON's decimal grammar.
`nan`, `inf`, hexadecimal numbers, and dates are not TQL literals. A TOML date/time
can be copied from an existing node; a date written as a string stays a string.
Duplicate keys in object literals are rejected.

Operators, from lowest to highest precedence:

| Operators | Meaning |
| --- | --- |
| `\|` | Pipe; keeps the same document root `$` |
| `??` | First value that is neither missing nor `null`; retains `false` |
| `or`, `\|\|` | Short-circuit logical OR |
| `and`, `&&` | Short-circuit logical AND |
| `==`, `!=` | Recursive value equality |
| `<`, `<=`, `>`, `>=` | Comparison of numbers or strings of the same type |
| `+`, `-` | Arithmetic; `+` also concatenates strings/arrays and shallow-merges objects |
| `*`, `/`, `%` | Multiplication, division, integer remainder |
| `not`, `!`, unary `+`, `-` | Logical negation and numeric signs |

Use parentheses to group expressions. Binary operators associate to the left.
Only `false` and `null` are false; `0`, `""`, `[]`, and `{}` are true. `and` and
`or` return booleans. A filter matches if any value produced by its predicate is
true.

Numbers are exact: `0.1 + 0.2` is `0.3`, and large integers do not pass through
`float64`. Division must produce a terminating decimal: `1 / 8` succeeds, while
`1 / 3` returns an error rather than rounding. Arithmetic limits include 10,000
digits, an absolute exponent of 10,000, and a bound on intermediate rational
sizes. Reading a number does not require arithmetic conversion. Numeric equality
compares values (`1 == 1.0`); object equality ignores key order.

Whitespace after a dot matters: `. * 2` multiplies the current value, while `.*`
selects the current container's children.

## Assignment

```tql
foo.bar = 1
baz[1].name = 3
obj = [1,2,3]
objs[name == "foo"] = "abc"
objs[name == "foo"].enabled = true
```

`=` evaluates its right side against the **statement's input** and assigns that
value to every target. The input is the document root outside a loop and the
current node inside a `for` body. `|=` evaluates its right side separately against
**each target's current value**:

```tql
users[].limit = $.default_limit
users[].limit |= . + 1
users[].limit += 1
users[].label |= . + "!"
```

`+=`, `-=`, `*=`, and `/=` combine the old target value with a right side evaluated
against the statement's input. `??=` fills a missing or `null` value; it does not
evaluate the right side for an existing non-null target.

All targets and right sides within one statement are evaluated from a snapshot
of the tree **before** that statement. Updating one match does not affect another
match's calculation. Duplicate targets are coalesced; overlapping assignment
targets are rejected.

`=` and `??=` create missing intermediate objects:

```tql
server.http.port = 8080
users[name == "alice"].metadata.active = true
```

They can also replace `null` with a container. A newly created array must start at
index `0`. Append to an existing array by assigning at its length: `items[3] =
"next"` appends to a three-element array. Sparse arrays are rejected. A wildcard
or filter selecting nothing does not create any objects. Other assignment
operators do nothing to missing targets. An out-of-bounds index with `=` or
`??=` is an error.

`items += [value]` also appends, but replaces the **whole array**, so it can change
formatting inside it. Assign at the array's length to preserve the spelling and
formatting of existing elements.

Inserted entries also inherit local document style where possible, including
indentation, separator spacing, quotes, and collection layout. This applies to
new text; replacing a whole collection still replaces its contents. See
[insertion style](architecture.md#insertion-style) for the inference boundaries.

## Deletion

```tql
del(foo.bar)
del(items[0], items[-1])
del(users[disabled == true])
del(obj.*)
```

Targets are selected from the statement's original snapshot. Deleting several
array elements does not shift the indexes of other selected targets. Descendants
of an already selected parent are coalesced with that parent. A missing target
is a successful no-op.

Deleting a JSON or TOML root is forbidden; deleting a YAML root produces `null`.
Deleting the last child of a container preserves an empty container of its type.

## Programs and transactions

```tql
# TQL comments run to the end of the line.
server.port = 9090;
server.enabled = true;
del(server.legacy);
server
```

Separate simple statements with `;`. The last semicolon is optional; a newline
alone does not separate statements. Each statement sees the result of the
previous one. If any statement fails, the entire `Exec` call rolls back. Mutations
produce no output values; read expressions append their results to `Result.Values`.

Control-flow blocks have their own separator rules, described in
[scripts](scripts.md#conditions).

## Built-in functions

| Function | Result |
| --- | --- |
| `length`, `length(x)` | String length in Unicode code points, array/object length, or `0` for `null` |
| `keys`, `keys(x)` | Sorted object keys or array indexes |
| `values`, `values(x)` | Array of object values ordered by sorted keys |
| `type`, `type(x)` | `object`, `array`, `string`, `number`, `boolean`, `null`, or `datetime` |
| `first`, `first(x)` | First array element; an empty array produces no result |
| `last`, `last(x)` | Last array element; an empty array produces no result |
| `reverse`, `reverse(x)` | Reversed array |
| `sort`, `sort(x)` | Ascending array of numbers or strings of one type |
| `select(predicate)` | Current node if the predicate is true; otherwise an empty stream |
| `has(key)`, `has(x,key)` | Object key or nonnegative array index membership |
| `contains(value)`, `contains(x,value)` | Substring match or an equal whole element in an array |
| `startswith(text)`, `endswith(text)` | String prefix or suffix test |
| `test(pattern)` | Go/RE2 regular expression search in a string |
| `walk()`, `walk(x)` | Recursive postorder stream, including the selected root |
| `path()`, `path(x)` | Current document-node path as an array of keys/indexes |
| `key()`, `key(x)` | Last path segment, or `null` for the root |

Functions with optional input use the current input when it is omitted.
`startswith`, `endswith`, and `test` also accept `(input, argument)`.
`path` and `key` require nodes belonging to the document, not local value
snapshots. See [recursive traversal](scripts.md#recursive-traversal) for examples.
Functions do not invoke a shell, network services, or external programs.

## Grammar outline

This abbreviated grammar shows the language's main forms. Operator precedence is
listed above; block and selector behavior is explained in the scripting guide.

```ebnf
program     = { parameter }, { statement } ;
parameter   = "param", variable, [ "=", constant ], [ ";" ] ;
statement   = simple, [ ";" ] | conditional | for_loop | while_loop ;
simple      = "let", variable, "=", expression
            | "del(", expression, { ",", expression }, ")"
            | "break" | "continue"
            | expression, [ assignment, expression ] ;
conditional = "if", expression, block, [ "else", ( conditional | block ) ] ;
for_loop    = "for", variable, "in", expression, block ;
while_loop  = "while", expression, block ;
block       = "{", { statement }, "}" ;
assignment  = "=" | "|=" | "+=" | "-=" | "*=" | "/=" | "??=" ;
expression  = prefix, { binary_operator, prefix } ;
prefix      = literal | object | array | path | call
            | "(", expression, ")" | unary_operator, expression ;
step        = ".", ( identifier | "*" )
            | "[", ( string | integer | slice | "*" | expression ), "]"
            | "[]" ;
slice       = [ integer ], ":", [ integer ] ;
```

Parameter declarations require semicolons except at the end of the program.
They precede all ordinary statements and cannot occur inside blocks. A default
is a scalar literal (numbers may have a sign), or an array/object of constants.
See [input parameters](scripts.md#input-parameters) for binding rules.

Consecutive simple statements require semicolons; they may be omitted before `}`
or at the end of the program. Expression nesting is limited to 256 levels, block
nesting to 128 levels, and execution to 100,000 VM steps. User-defined
functions and full jq compatibility are not part of the language.
