package toml

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestInsertionInheritsLocalStyle(t *testing.T) {
	tests := []struct {
		name, source, path, key string
		value                   any
		want                    string
	}{
		{"literal CRLF", "title='before'\r\ncount=1 # keep\r\n", "", "label", "after", "title='before'\r\ncount=1 # keep\r\nlabel='after'\r\n"},
		{"no final newline", "title='before'", "", "label", "after", "title='before'\nlabel='after'"},
		{"compact array", "names=[1,2] # keep\n", "names", "-", syntax.Number("3"), "names=[1,2,3] # keep\n"},
		{"spaced array", "names=[ 1, 2 ]", "names", "-", syntax.Number("3"), "names=[ 1, 2, 3 ]"},
		{"spaces around comma", "names=[1 , 2 , ]", "names", "-", syntax.Number("3"), "names=[1 , 2 , 3 , ]"},
		{"trailing comma", "names=[1,2,]", "names", "-", syntax.Number("3"), "names=[1,2,3,]"},
		{"aligned equals", "a    = 1\nlong = 2\n", "", "new", syntax.Number("3"), "a    = 1\nlong = 2\nnew  = 3\n"},
		{"tabs", "\tleft\t=\t'before'\n", "", "right", "after", "\tleft\t=\t'before'\n\tright\t=\t'after'\n"},
		{"single quoted keys", "'left'='before'\n", "", "right", "after", "'left'='before'\n'right'='after'\n"},
		{"literal fallback", "left='before'\n", "", "right", "can't\nstop", "left='before'\nright=\"can't\\nstop\"\n"},
		{"key fallback", "'left'=1\n", "", "can't", syntax.Number("2"), "'left'=1\n\"can't\"=2\n"},
		{"nearest section", "root = \"outer\"\n[a]\nname='inner'\n[b]\nother = \"unrelated\"\n", "a", "new", "value", "root = \"outer\"\n[a]\nname='inner'\nnew='value'\n[b]\nother = \"unrelated\"\n"},
		{"empty section fallback", "root='outer'\n[a]\n", "a", "new", "value", "root='outer'\n[a]\nnew='value'\n"},
		{"compact inline", "x={a=1,b=2}\n", "x", "new", "yes", "x={a=1,b=2,new=\"yes\"}\n"},
		{"empty inline", "x={  }\n", "x", "new", syntax.Number("2"), "x={  new=2  }\n"},
		{"nested map template", "one={a='x',b=[1,2]}\n", "", "two", map[string]any{"a": "y", "b": []any{syntax.Number("3"), syntax.Number("4")}}, "one={a='x',b=[1,2]}\ntwo={a='y',b=[3,4]}\n"},
		{"multiline comment", "x=[\n    1, # keep\n    2,\n]\n", "x", "-", syntax.Number("3"), "x=[\n    1, # keep\n    2,\n    3,\n]\n"},
		{"comment without comma", "x=[1 # keep\n]", "x", "-", syntax.Number("2"), "x=[1, # keep\n  2\n]"},
		{"multiline tabs CRLF", "x=[\r\n\t1, # keep\r\n]\r\n", "x", "-", syntax.Number("2"), "x=[\r\n\t1, # keep\r\n\t2,\r\n]\r\n"},
		{"empty multiline comments", "x=[\n # keep\n]\n", "x", "-", syntax.Number("2"), "x=[\n # keep\n  2\n]\n"},
		{"nested array template", "x=[\n    1,\n]\n", "", "y", []any{syntax.Number("2"), syntax.Number("3")}, "x=[\n    1,\n]\ny=[\n    2,\n    3,\n]\n"},
		{"nested multiline array indentation", "x=[\n    [\n        1,\n    ],\n]\n", "x", "-", []any{syntax.Number("2"), syntax.Number("3")}, "x=[\n    [\n        1,\n    ],\n    [\n        2,\n        3,\n    ],\n]\n"},
		{"array table", "[[ 'products' ]] # keep\r\nname='Hammer'\r\n", "products", "-", map[string]any{"name": "Saw"}, "[[ 'products' ]] # keep\r\nname='Hammer'\r\n[[ 'products' ]]\r\nname='Saw'\r\n"},
		{"array table no final newline", "[[products]]\nname='Hammer'", "products", "-", map[string]any{"name": "Saw"}, "[[products]]\nname='Hammer'\n[[products]]\nname='Saw'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := parse(t, tt.source)
			parent := root
			if tt.path != "" {
				for _, key := range strings.Split(tt.path, ".") {
					parent = parent.Get(key)
				}
			}
			edits, err := (Format{}).Insert([]byte(tt.source), root, parent, tt.key, tt.value)
			got := apply(t, tt.source, edits, err)
			if got != tt.want {
				t.Fatalf("got  %q\nwant %q", got, tt.want)
			}
			result := parse(t, got)
			for _, key := range strings.Split(tt.path, ".") {
				if key != "" {
					result = result.Get(key)
				}
			}
			var inserted *syntax.Node
			if result.Kind == syntax.Array {
				inserted = result.Items[len(result.Items)-1]
			} else {
				inserted = result.Get(tt.key)
			}
			if inserted == nil || !reflect.DeepEqual(inserted.Interface(), tt.value) {
				t.Fatalf("inserted value: %#v, want %#v", inserted, tt.value)
			}
		})
	}
}
