package tql_test

import (
	"os"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestDocumentedExampleProgram(t *testing.T) {
	source, e := os.ReadFile("examples/update.tql")
	if e != nil {
		t.Fatal(e)
	}
	program, e := tql.Compile(string(source))
	if e != nil {
		t.Fatal(e)
	}
	for _, format := range []string{"json", "toml", "yaml"} {
		t.Run(format, func(t *testing.T) {
			source, e := os.ReadFile("examples/config." + format)
			if e != nil {
				t.Fatal(e)
			}
			doc, e := tql.Parse(source, format)
			if e != nil {
				t.Fatal(e)
			}
			result, e := doc.Execute(program)
			if e != nil {
				t.Fatal(e)
			}
			if !result.Changed {
				t.Fatal("example did not change source")
			}
			values, e := doc.Query(`[server.port, users[0].enabled, server.timeout]`)
			if e != nil {
				t.Fatal(e)
			}
			items := values[0].([]any)
			if items[0] != tql.Number("9090") || items[1] != true || items[2] != tql.Number("30") {
				t.Fatalf("values=%v", values)
			}
			again, e := doc.Execute(program)
			if e != nil {
				t.Fatal(e)
			}
			if again.Changed {
				t.Fatal("idempotent example changed bytes twice")
			}
		})
	}
}

func TestDocumentedMigrationScript(t *testing.T) {
	source, err := os.ReadFile("examples/migrate.tql")
	if err != nil {
		t.Fatal(err)
	}
	program, err := tql.Compile(string(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "toml", "yaml"} {
		t.Run(format, func(t *testing.T) {
			source, err := os.ReadFile("examples/config." + format)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := tql.Parse(source, format)
			if err != nil {
				t.Fatal(err)
			}
			result, err := doc.Execute(program)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Values) != 1 || result.Values[0] != tql.Number("1") {
				t.Fatalf("values=%v", result.Values)
			}
			values, err := doc.Query(`[server.port, users[0].enabled, users[1].role]`)
			if err != nil {
				t.Fatal(err)
			}
			got := values[0].([]any)
			if got[0] != tql.Number("8090") || got[1] != true || got[2] != "reader" {
				t.Fatalf("got=%v", values)
			}
		})
	}
}
