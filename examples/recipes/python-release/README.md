# Prepare a Python package release

Update static package metadata and one known dependency constraint while keeping
inline comments, mixed quote styles, and unrelated tool settings intact.
The dependency selector matches the complete old requirement; it leaves other
packages and an already-updated requirement alone.

This recipe assumes a static `[project].version` and the dependency spellings in
the fixture. For a real project, choose compatible versions and adapt the exact
requirement selector. Regenerate your package manager's lockfile separately.
The version numbers here demonstrate a migration rather than recommend releases.

From the repository root, preview the edit:

```sh
tql --query-file examples/recipes/python-release/migrate.tql \
  examples/recipes/python-release/pyproject.toml
```

Apply it to your own file after adapting the script:

```sh
tql -i --query-file examples/recipes/python-release/migrate.tql pyproject.toml
```

The complete [input](pyproject.toml), [script](migrate.tql), and
[expected output](expected/pyproject.toml) are checked in. The script is idempotent:
running it again produces no changes. These exact bytes and a second run are
verified by `go test ./...`.

```diff
--- pyproject.toml
+++ expected/pyproject.toml
@@ -5,9 +5,9 @@
 [project]
 name = "example-report-cli"
-version = "0.8.0" # Keep this release marker for the changelog script.
+version = "0.9.0" # Keep this release marker for the changelog script.
 description = "Generate local reports"
-requires-python = ">=3.10"
+requires-python = ">=3.11"
 dependencies = [
-    "httpx>=0.27,<0.28", # Only the HTTP client needs a new version range.
+    "httpx>=0.28,<0.29", # Only the HTTP client needs a new version range.
     'rich>=13,<15',      # Leave unrelated dependency spelling alone.
 ]
```

String values being replaced may receive quotes; other source ranges retain
their original spelling. [Field reference](https://packaging.python.org/en/latest/guides/writing-pyproject-toml/): Python project metadata.

[All recipes](../README.md)
