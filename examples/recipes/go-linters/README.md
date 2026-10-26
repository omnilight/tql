# Extend a golangci-lint configuration

Set a five-minute analysis timeout and enable `misspell`, `revive`, and `gocritic`
without duplicates. This is useful when rolling a common set of checks across
repositories that already have different linters enabled. Existing comments and
the `misspell` settings remain in place; the new entries use the original block
sequence's indentation.

The input uses the golangci-lint v2 schema and already has a `linters.enable`
array. Choose checks that suit your project and resolve any overlap with its
`linters.disable` list before using this script there. The script computes the
append index instead of replacing the entire list.

From the repository root, preview the edit:

```sh
tql --query-file examples/recipes/go-linters/migrate.tql \
  examples/recipes/go-linters/.golangci.yaml
```

Apply it to your own file after adapting the script:

```sh
tql -i --query-file examples/recipes/go-linters/migrate.tql .golangci.yaml
```

The complete [input](.golangci.yaml), [script](migrate.tql), and
[expected output](expected/.golangci.yaml) are checked in. The script is idempotent:
running it again produces no changes. These exact bytes and a second run are
verified by `go test ./...`.

```diff
--- .golangci.yaml
+++ expected/.golangci.yaml
@@ -3,5 +3,5 @@

 run:
-  timeout: 2m # CI needs enough time for the first, uncached analysis.
+  timeout: "5m" # CI needs enough time for the first, uncached analysis.
   tests: true

@@ -11,4 +11,6 @@
     - bodyclose # Existing choices and their comments stay in place.
     - misspell
+    - revive
+    - gocritic
   settings:
     misspell:
```

String values being replaced may receive quotes; other source ranges retain
their original spelling. [Field reference](https://golangci-lint.run/docs/configuration/file/): golangci-lint configuration.

[All recipes](../README.md)
