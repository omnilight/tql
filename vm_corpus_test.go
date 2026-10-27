package tql

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVMRecipeParity(t *testing.T) {
	files, err := filepath.Glob("examples/recipes/*/migrate.tql")
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range files {
		dir := filepath.Dir(script)
		scriptBytes, err := os.ReadFile(script)
		if err != nil {
			t.Fatal(err)
		}
		inputs, err := filepath.Glob(filepath.Join(dir, "*"))
		if err != nil {
			t.Fatal(err)
		}
		for _, input := range inputs {
			ext := strings.ToLower(filepath.Ext(input))
			if ext != ".json" && ext != ".toml" && ext != ".yaml" {
				continue
			}
			source, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			format := strings.TrimPrefix(ext, ".")
			if format == "yaml" {
				format = "yaml"
			}
			t.Run(filepath.Base(dir)+"/"+filepath.Base(input), func(t *testing.T) {
				p, err := Compile(string(scriptBytes))
				if err != nil {
					t.Fatal(err)
				}
				astDoc, err := Parse(source, format)
				if err != nil {
					t.Fatal(err)
				}
				vmDoc, err := Parse(source, format)
				if err != nil {
					t.Fatal(err)
				}
				ast, err1 := astDoc.executeAST(p, true, execOptions{})
				vm, err2 := vmDoc.executeVM(p, true, execOptions{})
				if (err1 == nil) != (err2 == nil) {
					t.Fatalf("error mismatch: AST=%v VM=%v", err1, err2)
				}
				if err1 != nil {
					if err1.Error() != err2.Error() {
						t.Fatalf("error text: AST=%q VM=%q", err1, err2)
					}
					return
				}
				if string(ast.Bytes) != string(vm.Bytes) {
					t.Fatalf("bytes differ:\nAST:\n%s\nVM:\n%s", ast.Bytes, vm.Bytes)
				}
				if len(ast.Values) != len(vm.Values) {
					t.Fatalf("value count: AST=%v VM=%v", ast.Values, vm.Values)
				}
			})
		}
	}
}
