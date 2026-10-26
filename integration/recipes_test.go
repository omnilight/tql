package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPracticalRecipeFiles(t *testing.T) {
	for _, recipe := range []struct{ name, filename string }{
		{"python-release", "pyproject.toml"},
		{"go-linters", ".golangci.yaml"},
		{"compose-rollout", "compose.yaml"},
		{"node-release", "package.json"},
	} {
		t.Run(recipe.name, func(t *testing.T) {
			root := filepath.Join("..", "examples", "recipes", recipe.name)
			source := read(t, filepath.Join(root, recipe.filename))
			expected := read(t, filepath.Join(root, "expected", recipe.filename))
			program := read(t, filepath.Join(root, "migrate.tql"))
			dir := t.TempDir()
			input := filepath.Join(dir, recipe.filename)
			script := filepath.Join(dir, "migrate.tql")
			write(t, input, []byte(source))
			write(t, script, []byte(program))
			before := stat(t, input)
			wantResult(t, run(t, dir, "", "--check", "--query-file", script, input), 1, "")
			if read(t, input) != source || !os.SameFile(before, stat(t, input)) {
				t.Fatal("preview changed the input file")
			}
			wantResult(t, run(t, dir, "", "-i", "--query-file", script, input), 0, "")
			if got := read(t, input); got != expected {
				t.Fatalf("recipe output differs from the documented bytes:\ngot:\n%s\nwant:\n%s", got, expected)
			}
			after := stat(t, input)
			wantResult(t, run(t, dir, "", "-i", "--query-file", script, input), 0, "")
			wantResult(t, run(t, dir, "", "--check", "--query-file", script, input), 0, "")
			if got := stat(t, input); !os.SameFile(after, got) || !after.ModTime().Equal(got.ModTime()) || read(t, input) != expected {
				t.Fatal("repeated migration rewrote the input file")
			}
			noTemps(t, dir)
		})
	}
}
