# Scripting guide

TQL scripts combine variables, conditions, tree traversal, and edits in a single
transaction. The CLI reads scripts with `--query-file`; the Go API accepts them
through `Compile`, `Exec`, or `Execute`.

```tql
let $delta = 10;
let $changed = 0;

for $server in servers[] {
    if $server.enabled {
        $server.port += $delta;
        $changed += 1;
    } else {
        $server.port = 0;
    }
}

$changed
```

```sh
tql --query-file migration.tql config.yaml          # print the final document
tql -i --query-file migration.tql config.yaml       # update the input file
tql --values --query-file migration.tql config.yaml # print read results: the counter
```

## Input parameters

Declare a script's inputs before its first ordinary statement:

```tql
param $service;
param $image;
param $replicas = 2;

services[$service].image = $image;
services[$service].deploy.replicas = $replicas;
```

```sh
tql --arg service=api --arg image=api:v2 --argjson replicas=3 \
    --query-file rollout.tql compose.yaml
```

The Go APIs accept the same inputs through `tql.WithParams`; see the
[library guide](library.md#input-parameters). Parameter names use the same
identifier rules as local variables. CLI and Go names omit the leading `$`.

An input without `=` is required, even if its only use is in an untaken branch.
An input with `=` uses its default only when the caller omits the name. Explicit
`null`, `false`, zero, and empty strings override the default. Missing required
inputs and unknown supplied names fail before any script statement runs.

Defaults are constant values: strings, numbers (including a leading sign),
booleans, `null`, and nested arrays or objects of constants. Paths, variables,
function calls, and calculations are not defaults. Use a local variable for
document-dependent fallback:

```tql
param $timeout = null;
let $effective_timeout = $timeout ?? server.timeout ?? 30;
server.timeout = $effective_timeout;
```

Declarations must be at the top level, before all ordinary statements, and have
unique names. Separate them with semicolons. `param` remains a valid document
field name: `param.name` and `param = 1` are ordinary paths and assignments.
A declaration alone neither mutates the document nor produces a read result.
There are no parameter type annotations; ordinary expression and target-format
checks apply when the script uses a value.

Parameters and their descendants are read-only. Assignment, compound assignment,
and deletion of selected parameter nodes fail, including through references from
`for` or `walk`. Make a local snapshot when an algorithm needs a mutable value:

```tql
param $settings;
let $local = $settings;
$local.timeout = 10; # changes only the local copy
```

Parameters share the script's outer lexical scope. `let` cannot redeclare a
parameter in that scope; an inner declaration or loop variable can shadow it.
Each execution receives independent values, including arrays and objects used as
defaults. Parameters do not persist on a `Document` or a compiled `Program`.
They are data, never evaluated as TQL source or interpolated into string literals.

## Variables and scope

```tql
let $step = 2;
let $counter = 0;
let $old = server.port;
let $settings = {enabled: true, ports: [80,443]};
$counter += $step;
$settings.ports[0] = 8080;
del($settings.enabled);
server.port = $old + $counter
```

`let $name = expression` stores a **value snapshot**, not a reference to its source
node. Changing `$settings` or `$old` alone does not change the document. Local
objects and arrays support assignment, compound assignment, nested updates,
insertions, and deletions. `del($variable)` sets it to `null`; it does not remove
the declaration.

Scope is lexical. Each block and loop iteration has its own declarations. An
inner declaration can shadow an outer name; assignment without `let` updates
the nearest existing declaration. Redeclaring a name in the same scope or reading
an unknown variable is an error. Variables do not survive an execution: a reused
compiled program starts with a fresh environment every time.

```tql
let $x = 1;
if true {
    let $x = 10;
    $x += 1;
    $x;                 # 11
}
$x                      # 1
```

A variable name is `$` followed immediately by a TQL identifier. Bare `$` and
`$.field` refer to the document root.

## Conditions

```tql
if server.port < 1024 {
    server.privileged = true;
} else if server.port < 10000 {
    server.privileged = false;
} else {
    del(server.privileged);
}
```

Conditions are ordinary expressions; parentheses are optional. Only `false` and
`null` are false. An empty result stream is treated as `null`; multiple condition
results are an error. The unselected branch is not evaluated.

Separate simple statements with `;`. A semicolon is optional before `}` and at
the end of the file. No additional semicolon is needed after an `if`, `for`, or
`while` block. A newline alone does not separate simple statements. `#` begins a
comment that ends at the next newline.

## Iterate over selected nodes

```tql
for $item in items[] {
    if $item.disabled {
        del($item);
        continue;
    }
    $item.price *= 2;
}
```

`for $name in expression { ... }` consumes a **stream**. Use `items[]` for array
elements or `settings.*` for object values. `items` by itself produces one value
containing the whole array, so it would run the loop only once.

If a selected node belongs to the document or a local variable, the loop variable
is a **live reference** to it. Assigning to `$item` or one of its fields updates
that tree. For computed values, such as `for $i in [1,2,3][]`, the variable holds
an independent local value.

The traversal's target list is fixed before the loop starts. Objects use source
field order; arrays use increasing indexes. After a deletion, references for later
iterations adjust to continue pointing to the original elements. Deleted future
targets are skipped, and newly inserted nodes are not visited. Replacing a parent
cancels future visits to its former descendants. Accessing an already deleted
node through `$item` returns an error; use `continue` after deleting the current
node when appropriate.

Nested loops have independent variables and target lists. `break` exits the
nearest loop; `continue` begins its next iteration.

## Current input and root

Inside a `for` body, `.` is the selected node. Relative paths and ordinary
expressions, including `let`, conditions, and the right side of `=`, use that
input. `$` always refers to the file's root:

```tql
for $server in servers[] {
    let $old = .port;
    .port = $old + $.default_delta;
    .enabled = true;
}
```

`$server.port` explicitly addresses the same node. A nested loop changes `.` only
for its own body. Outside loops, `.` refers to the root again. `|=` evaluates its
right side against the **assignment target itself**:

```tql
for $server in servers[] {
    .port = .port + 1;  # right side sees the server
    .port |= . + 1;    # right side sees the numeric port
}
```

Use `$.other` to reach another part of the file from a loop; `other` is relative
to the current node.

## Recursive traversal

```tql
for $node in walk(.) {
    if type($node) == "number" {
        $node |= . + 1;
    }
}
```

`walk()` traverses the current input; `walk(expression)` traverses the expression's
nodes. The stream includes containers and the selected root. Traversal is
**postorder**: children in source order, then their parent. This allows a container
to be updated after its descendants have been visited.

For an independent update of each selected value, a single statement is usually
faster than a loop:

```tql
walk(.) | select(type == "number") |= . + 1
```

Non-overlapping replacements within one statement are batched into one set of
source edits. See [performance](performance.md) for sequential-script costs.

`path()` returns the current node's path as an array of keys and indexes. `key()`
returns its last segment, or `null` for the root. `path($node)` and `key($node)`
address an explicit node. These functions require document nodes and report their
**current** addresses, including index changes after deletions. The root's path
is `[]`.

```tql
for $node in walk(.) {
    if type($node) == "number" {
        {path: path($node), value: $node};
    }
}
```

Local value snapshots have no address in the source document, so `path` and `key`
return an error for them. `walk` also supports local trees.

## Dynamic keys and indexes

```tql
let $key = "host";
server[$key] = "localhost";
let $i = 1;
items[$i - 1].enabled = true
```

Variable selectors such as `[$key]`, `[$item.key]`, and `[$i - 1]` are evaluated
once against the indexed container. They must produce one string or an integer
within Go's `int` range. Negative indexes count from the end. Appending requires
an index equal to the array's length; sparse arrays are rejected. Coalescing is
also supported: `obj[$key ?? "default"]`.

Comparisons and logical expressions remain **filters**:

```tql
let $name = "alice";
users[name == $name].enabled = true
```

A fractional, boolean, missing, or overflowing dynamic index is an error; it never
falls back to selecting every element.

## While loops and execution limits

```tql
let $i = 0;
while $i < length(items) {
    let $index = $i;
    $i += 1;
    if $index == 1 { continue; }
    if $index == 10 { break; }
    items[$index] = $index;
}
```

`while` evaluates its condition before each iteration. Each execution is limited
to 100,000 VM steps, including statements and loop checks/transitions.
This is not a count of document nodes. An empty infinite loop also fails at the
limit. Block nesting is limited to 128 levels and expression nesting to 256.
The step limit is not a time or memory bound for an individual expression.

## Transactions and API behavior

An error in any branch or iteration rolls back **all document changes** made by
that execution. Earlier read results are not returned as partial output. Local
variables are discarded when execution finishes. Format implementations remain
responsible for preserving source syntax during edits.

`Program.Mutates()` conservatively treats every assignment or deletion as a
potential mutation, including changes to local variables. A `let` declaration alone is read-only.
Run programs containing assignments or deletions with
`Exec`/`Execute` rather than `Query`. `Result.Changed` reports only a change in the
source bytes: updating local variables alone leaves it false. Use `--values` in
the CLI to print read results from these programs.

See the [language reference](syntax.md), [CLI guide](cli.md), and
[Go API guide](library.md) for the surrounding APIs.
