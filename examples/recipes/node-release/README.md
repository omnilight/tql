# Prepare Node package metadata and hooks

Set a release version and Node engine baseline, enable test coverage, replace an
old `prepublish` hook with `prepublishOnly`, and keep the original four-space JSON
layout. This shows assignment, insertion, and deletion in the same transaction.

The fixture is a private example package. Adapt the scripts, Node baseline, and
version to your package; this example does not run commands, install dependencies,
regenerate a lockfile, or publish anything. `engines` declares compatibility and
is normally advisory unless npm is configured to enforce it.

From the repository root, preview the edit:

```sh
tql --query-file examples/recipes/node-release/migrate.tql \
  examples/recipes/node-release/package.json
```

Apply it to your own file after adapting the script:

```sh
tql -i --query-file examples/recipes/node-release/migrate.tql package.json
```

The complete [input](package.json), [script](migrate.tql), and
[expected output](expected/package.json) are checked in. The script is idempotent:
running it again produces no changes. These exact bytes and a second run are
verified by `go test ./...`.

Deletion leaves the surrounding whitespace in place, including the blank line
where the old hook used to be.

```diff
--- package.json
+++ expected/package.json
@@ -1,12 +1,13 @@
 {
     "name": "example-report-tools",
-    "version": "1.4.0",
+    "version": "1.5.0",
     "private": true,
     "type": "module",
-    "engines": { "node": ">=20" },
+    "engines": { "node": ">=22" },
     "scripts": {
-        "test": "node --test",
-        "prepublish": "npm test",
-        "build": "node scripts/build.mjs"
+        "test": "node --test --experimental-test-coverage",
+
+        "build": "node scripts/build.mjs",
+        "prepublishOnly": "npm test && npm run build"
     }
 }
```

String values being replaced may receive quotes; other source ranges retain
their original spelling. [Field reference](https://docs.npmjs.com/cli/configuring-npm/package-json/): npm package.json fields.

[All recipes](../README.md)
