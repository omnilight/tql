package tql_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vasileknik76/tql"
)

// jq ports are checked against a fixed input so both supported and partially
// supported patterns stay explicit.
func TestJQPorts(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("examples", "jq", "input.json"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		script string
		values []any
		want   string
	}{
		{
			name:   "select and project",
			script: "select.tql",
			values: []any{
				map[string]any{"name": "alice", "age": tql.Number("31")},
				map[string]any{"name": "carol", "age": tql.Number("40")},
			},
		},
		{
			name:   "recursive names",
			script: "walk.tql",
			values: []any{"alice", "bob", "carol"},
		},
		{
			name:   "map values",
			script: "map-values.tql",
			want:   `{"users":[{"name":"alice","age":31,"password":"x"},{"name":"bob","age":25},{"name":"carol","age":40,"password":"y"}],"settings":{"a":2,"b":3},"nums":[1,2,3,4]}` + "\n",
		},
		{
			name:   "delete secrets",
			script: "delete-secrets.tql",
			want:   `{"users":[{"name":"alice","age":31},{"name":"bob","age":25},{"name":"carol","age":40}],"settings":{"a":1,"b":2},"nums":[1,2,3,4]}` + "\n",
		},
		{
			name:   "find key paths",
			script: "find-key.tql",
			values: []any{
				map[string]any{"path": []any{"users", tql.Number("0"), "name"}, "value": "alice"},
				map[string]any{"path": []any{"users", tql.Number("1"), "name"}, "value": "bob"},
				map[string]any{"path": []any{"users", tql.Number("2"), "name"}, "value": "carol"},
			},
		},
		{
			name:   "numeric fold",
			script: "sum.tql",
			values: []any{tql.Number("10")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			script, err := os.ReadFile(filepath.Join("examples", "jq", test.script))
			if err != nil {
				t.Fatal(err)
			}
			program, err := tql.Compile(string(script))
			if err != nil {
				t.Fatal(err)
			}
			document, err := tql.Parse(source, "json")
			if err != nil {
				t.Fatal(err)
			}
			result, err := document.Execute(program)
			if err != nil {
				t.Fatal(err)
			}

			if test.values != nil {
				if result.Changed {
					t.Fatal("read-only port changed the document")
				}
				if !reflect.DeepEqual(result.Values, test.values) {
					t.Fatalf("values = %#v, want %#v", result.Values, test.values)
				}
				return
			}
			if !result.Changed {
				t.Fatal("mutation port did not change the document")
			}
			if got := string(document.Bytes()); got != test.want {
				t.Fatalf("document bytes = %s, want %s", got, test.want)
			}
		})
	}
}
