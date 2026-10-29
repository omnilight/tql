package tql_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestBytecodeExampleArtifacts(t *testing.T) {
	source, err := os.ReadFile("examples/update.tql")
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := os.ReadFile("examples/bytecode/update.tqla")
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile("examples/bytecode/update.tqlc")
	if err != nil {
		t.Fatal(err)
	}

	compiled, err := tql.Compile(string(source))
	if err != nil {
		t.Fatal(err)
	}
	if got := compiled.Disassemble(); got != string(assembly) {
		t.Fatalf("examples/bytecode/update.tqla is stale; regenerate it from examples/update.tql")
	}
	generated, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, image) {
		t.Fatalf("examples/bytecode/update.tqlc is stale; regenerate it from examples/update.tql")
	}

	loaded, err := tql.LoadProgram(image)
	if err != nil {
		t.Fatal(err)
	}
	assembled, err := tql.Assemble(string(assembly))
	if err != nil {
		t.Fatal(err)
	}

	for _, format := range []string{"json", "toml", "yaml"} {
		t.Run(format, func(t *testing.T) {
			source, err := os.ReadFile("examples/config." + format)
			if err != nil {
				t.Fatal(err)
			}
			run := func(program *tql.Program) []byte {
				t.Helper()
				doc, err := tql.Parse(source, format)
				if err != nil {
					t.Fatal(err)
				}
				result, err := doc.Execute(program)
				if err != nil {
					t.Fatal(err)
				}
				if !result.Changed {
					t.Fatal("example did not change the document")
				}
				return result.Bytes
			}
			want := run(compiled)
			if got := run(loaded); !bytes.Equal(got, want) {
				t.Fatalf("image result differs from source result:\n%s\n%s", got, want)
			}
			if got := run(assembled); !bytes.Equal(got, want) {
				t.Fatalf("assembly result differs from source result:\n%s\n%s", got, want)
			}
		})
	}
}
