# Parameterized deployment

`rollout.tql` updates a selected service's image and replica count. Service and
image are required inputs; replicas defaults to `2`. The fixture already contains
both services, so the script only replaces the selected scalars.

From the repository root, run either command to preview a three-replica rollout:

```sh
tql --arg service=api --arg image=api:v2 --argjson replicas=3 \
    --query-file examples/parameters/rollout.tql examples/parameters/compose.yaml

tql --params-file examples/parameters/production.json \
    --query-file examples/parameters/rollout.tql examples/parameters/compose.yaml
```

The result matches `expected.yaml`. Add `-i` before the other arguments to save
changes to a copy of the input file. Repeating the same rollout changes no bytes.
The worker configuration and the API's image comment remain in place.

Explicit flags override file values regardless of flag order. For a different
release, keep the same script and file, and add `--arg image=api:v3`. To use the
script default of two replicas, omit `--argjson replicas=3` from the first command.

The same script works through `Compile` and `Execute(program, WithParams(...))`;
see [Go parameter binding](../../docs/library.md#input-parameters).
