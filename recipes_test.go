package tql_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vasileknik76/tql"
)

// Recipe expectations are checked in so changes to formatting are reviewable.
func TestPracticalRecipes(t *testing.T) {
	recipes := map[string]struct {
		filename string
		format   string
		verify   string
	}{
		"python-release": {
			filename: "pyproject.toml",
			format:   "toml",
			verify:   `project.version == "0.9.0" and project["requires-python"] == ">=3.11" and project.dependencies == ["httpx>=0.28,<0.29", "rich>=13,<15"] and project.scripts.report == "example_report.cli:main" and tool.ruff["line-length"] == 100`,
		},
		"go-linters": {
			filename: ".golangci.yaml",
			format:   "yaml",
			verify:   `run.timeout == "5m" and run.tests == true and linters.enable == ["bodyclose", "misspell", "revive", "gocritic"] and linters.settings.misspell.locale == "US"`,
		},
		"compose-rollout": {
			filename: "compose.yaml",
			format:   "yaml",
			verify:   `services.web.image == "nginx:1.28-alpine" and services.admin.image == "nginx:1.28-alpine" and services.web.logging.options["max-size"] == "20m" and services.admin.logging.options["max-file"] == "3" and services.cache.image == "redis:7-alpine" and services.web.ports == ["8080:80"]`,
		},
		"node-release": {
			filename: "package.json",
			format:   "json",
			verify:   `.version == "1.5.0" and engines.node == ">=22" and scripts.test == "node --test --experimental-test-coverage" and scripts.prepublishOnly == "npm test && npm run build" and not has(scripts, "prepublish") and scripts.build == "node scripts/build.mjs"`,
		},
	}
	root := filepath.Join("examples", "recipes")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		recipe, ok := recipes[name]
		if !ok {
			t.Errorf("recipe %q has no semantic expectations", name)
			continue
		}
		count++
		t.Run(name, func(t *testing.T) {
			directory := filepath.Join(root, name)
			source := readRecipeFile(t, filepath.Join(directory, recipe.filename))
			script := readRecipeFile(t, filepath.Join(directory, "migrate.tql"))
			expected := readRecipeFile(t, filepath.Join(directory, "expected", recipe.filename))
			program, err := tql.Compile(string(script))
			if err != nil {
				t.Fatal(err)
			}
			document, err := tql.Parse(source, recipe.format)
			if err != nil {
				t.Fatal(err)
			}
			result, err := document.Execute(program)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Changed || len(result.Values) != 0 {
				t.Fatalf("expected a mutation without read output, got %+v", result)
			}
			if got := document.Bytes(); !bytes.Equal(got, expected) {
				t.Fatalf("unexpected document bytes\ngot:\n%s\nwant:\n%s", got, expected)
			}
			values, err := document.Query(recipe.verify)
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != 1 || values[0] != true {
				t.Fatalf("semantic checks failed: %v", values)
			}
			// Parse again as a separate CLI invocation would, then reuse the program.
			again, err := tql.Parse(expected, recipe.format)
			if err != nil {
				t.Fatal(err)
			}
			second, err := again.Execute(program)
			if err != nil {
				t.Fatal(err)
			}
			if second.Changed || !bytes.Equal(again.Bytes(), expected) {
				t.Fatal("running the documented migration twice changed its output")
			}
		})
	}
	if count != len(recipes) {
		t.Errorf("found %d recipe directories, want %d", count, len(recipes))
	}
}

func readRecipeFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
