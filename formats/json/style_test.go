package json

import (
	stdjson "encoding/json"
	"reflect"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestInsertionStyle(t *testing.T) {
	cases := []struct {
		name, source, path, key, want string
		value                         any
	}{
		{"space before commas", `{ "a" : 1 ,  "b" : 2 }`, "", "c", `{ "a" : 1 ,  "b" : 2 ,  "c" : 3 }`, 3},
		{"tabs around separators", "{\"a\"\t:\t1\t,\t\"b\"\t:\t2}", "", "c", "{\"a\"\t:\t1\t,\t\"b\"\t:\t2\t,\t\"c\"\t:\t3}", 3},
		{"empty padded array", `[ ]`, "", "-", `[ 1 ]`, 1},
		{"empty inherits four spaces", "{\n    \"empty\": {}\n}", "empty", "x", "{\n    \"empty\": {\n        \"x\": 1\n    }\n}", 1},
		{"empty inherits tabs CRLF", "{\r\n\t\"empty\" : []\r\n}\t", "empty", "-", "{\r\n\t\"empty\" : [\r\n\t\t1\r\n\t]\r\n}\t", 1},
		{"bare CR inherits indentation", "{\r    \"empty\": {}\r}", "empty", "x", "{\r    \"empty\": {\r        \"x\": 1\r    }\r}", 1},
		{"bare CR nested structure", "{\r\t\"a\" : 1\r}", "", "x", "{\r\t\"a\" : 1,\r\t\"x\" : [\r\t\t2,\r\t\t3\r\t]\r}", []any{2, 3}},
		{"empty with existing multiline gap", "{\n    \"empty\": {\n    }\n}", "empty", "x", "{\n    \"empty\": {\n        \"x\": 1\n    }\n}", 1},
		{"padded nested map and array", `{ "a" : 1 }`, "", "b", `{ "a" : 1, "b" : { "items" : [ 2, 3 ] } }`, map[string]any{"items": []any{2, 3}}},
		{"compact object template in multiline array", "[\n    {\"id\": 1}\n]", "", "-", "[\n    {\"id\": 1},\n    {\"a\": 2,\"b\": [3,4]}\n]", map[string]any{"a": 2, "b": []any{3, 4}}},
		{"multiline object template in compact array", "[{\n  \"id\": 1\n}]", "", "-", "[{\n  \"id\": 1\n},{\n  \"a\": 2\n}]", map[string]any{"a": 2}},
		{"new nested multiline containers", "{\r\n    \"a\": 0\r\n}", "", "b", "{\r\n    \"a\": 0,\r\n    \"b\": {\r\n        \"items\": [\r\n            1,\r\n            2\r\n        ]\r\n    }\r\n}", map[string]any{"items": []any{1, 2}}},
		{"blank lines not copied", "{\n  \"a\": 1,\n\n  \"b\": 2\n}", "", "c", "{\n  \"a\": 1,\n\n  \"b\": 2,\n  \"c\": 3\n}", 3},
		{"empty preserves blank closing lines", "{\n\n}", "", "x", "{\n  \"x\": 1\n\n}", 1},
		{"empty array borrows sibling", "{\n    \"old\": [1, 2],\n    \"empty\": []\n}", "empty", "-", "{\n    \"old\": [1, 2],\n    \"empty\": [3]\n}", 3},
		{"empty object borrows following sibling", `{"empty":{},"old":{ "x" : 1 }}`, "empty", "a", `{"empty":{ "a" : 2 },"old":{ "x" : 1 }}`, 2},
		{"empty root nested fallback", `{}`, "", "x", `{"x":{"a":[1,2]}}`, map[string]any{"a": []any{1, 2}}},
		{"nearest sibling template", `[{"x":1}, { "x" : 2 }]`, "", "-", `[{"x":1}, { "x" : 2 }, { "a" : 3, "b" : 4 }]`, map[string]any{"a": 3, "b": 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := parse(t, tc.source)
			parent := n
			if tc.path != "" {
				parent = n.Get(tc.path)
			}
			edits, err := (Format{}).Insert([]byte(tc.source), n, parent, tc.key, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			got, err := syntax.Apply([]byte(tc.source), edits)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
			if !stdjson.Valid(got) {
				t.Fatalf("invalid JSON: %q", got)
			}
			updated := parse(t, string(got))
			if tc.path != "" {
				updated = updated.Get(tc.path)
			}
			var inserted *syntax.Node
			if updated.Kind == syntax.Array {
				inserted = updated.Items[len(updated.Items)-1]
			} else {
				inserted = updated.Get(tc.key)
			}
			compact, err := encoded(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			want := parse(t, string(compact))
			if !reflect.DeepEqual(inserted.Interface(), want.Interface()) {
				t.Fatal("insertion changed value semantics")
			}
		})
	}
}

func TestInsertRejectsScalar(t *testing.T) {
	for _, source := range []string{"1", "null", `"x"`} {
		n := parse(t, source)
		if _, err := (Format{}).Insert([]byte(source), n, n, "x", 1); err == nil {
			t.Fatalf("accepted scalar %q", source)
		}
	}
}
