package yaml

import (
	"reflect"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestInsertFollowsLocalStyle(t *testing.T) {
	cases := []struct {
		name, src, path, key string
		value                any
		want                 string
	}{
		{"quoted CRLF", "'title': 'before'\r\n'count': 1 # keep\r\n", "", "label", "after", "'title': 'before'\r\n'count': 1 # keep\r\n'label': 'after'\r\n"},
		{"no final newline", "'title': 'before'", "", "label", "after", "'title': 'before'\n'label': 'after'"},
		{"plain sequence", "names:\n    - old\n    - keep # keep\n", "names", "", "new", "names:\n    - old\n    - keep # keep\n    - new\n"},
		{"local quote wins", "other: \"old\"\nlocal:\n    'name': 'before'\n", "local", "next", "it's", "other: \"old\"\nlocal:\n    'name': 'before'\n    'next': 'it''s'\n"},
		{"unsafe plain scalar", "name: before\n", "", "next", "true", "name: before\nnext: \"true\"\n"},
		{"unsafe plain key", "name: before\n", "", "a: b", "after", "name: before\n\"a: b\": after\n"},
		{"dash spacing", "-   old\n-   keep\n", "", "", "new", "-   old\n-   keep\n-   new\n"},
		{"noncompact map", "rows:\n  -\n      name: old\n", "rows", "", map[string]any{"name": "new"}, "rows:\n  -\n      name: old\n  -\n      name: new\n"},
		{"empty flow comment", "{ # keep\n}\n", "", "a", true, "{ # keep\n  \"a\": true\n}\n"},
		{"flow tight", "{'a':1,'b':2}", "", "c", syntax.Number("3"), "{'a':1,'b':2,'c':3}"},
		{"flow padding", "{ a: 1, b: 2 }", "", "c", syntax.Number("3"), "{ a: 1, b: 2, c: 3 }"},
		{"flow trailing comma", "[old, keep, ]", "", "", "new", "[old, keep, new, ]"},
		{"flow comment", "[old, # keep\n]", "", "", "new", "[old, # keep\n  new,\n]"},
		{"flow comment missing comma", "[old # keep\n]", "", "", "new", "[old, # keep\n  new\n]"},
		{"empty flow inherits quotes", "'name': 'old'\n'values': {}\n", "values", "next", "new", "'name': 'old'\n'values': {'next': 'new'}\n"},
		{"empty multiline", "x: {\n}\n", "x", "a", true, "x: {\n  a: true\n}\n"},
		{"nested block", "old:\n    name: before\n", "", "next", map[string]any{"name": "after", "enabled": true}, "old:\n    name: before\nnext:\n    name: after\n    enabled: true\n"},
		{"sequence map template", "rows:\n  - name: old\n    port: 80\n", "rows", "", map[string]any{"port": syntax.Number("81"), "name": "new"}, "rows:\n  - name: old\n    port: 80\n  - name: new\n    port: 81\n"},
		{"flow map template", "rows: [{ 'name': 'old', 'port': 80 }]\n", "rows", "", map[string]any{"port": syntax.Number("81"), "name": "new"}, "rows: [{ 'name': 'old', 'port': 80 },{ 'name': 'new', 'port': 81 }]\n"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			n := parse(t, tt.src)
			parent := n
			if tt.path != "" {
				parent = n.Get(tt.path)
			}
			out := insert(t, tt.src, parent, tt.key, tt.value)
			if out != tt.want {
				t.Fatalf("got  %q\nwant %q", out, tt.want)
			}
			got := parse(t, out)
			if tt.path != "" {
				got = got.Get(tt.path)
			}
			var v any
			if got.Kind == syntax.Array {
				v = got.Items[len(got.Items)-1].Interface()
			} else {
				v = got.Get(tt.key).Interface()
			}
			if !reflect.DeepEqual(v, tt.value) {
				t.Fatalf("value %#v != %#v", v, tt.value)
			}
		})
	}
}

func TestInsertedPlainStringsRemainStrings(t *testing.T) {
	for _, value := range []string{"true", "null", "1", "0xA", ".inf", "a: b", "a # b", "[x]", "---", "...", " lead", "tail ", "a\nb", "\x7f", "a,b", "foo:bar", "a'b", "a\"b", "日本語"} {
		for _, src := range []string{"old: before\n", "[before]", "old: 'before'\n", "old: {}\n"} {
			t.Run(src+value, func(t *testing.T) {
				n := parse(t, src)
				key := "new"
				if n.Kind == syntax.Array {
					key = ""
				}
				out := insert(t, src, n, key, value)
				n = parse(t, out)
				var v *syntax.Node
				if n.Kind == syntax.Array {
					v = n.Items[len(n.Items)-1]
				} else {
					v = n.Get(key)
				}
				if v.Kind != syntax.String || v.Value != value {
					t.Fatalf("%q: %#v", out, v.Interface())
				}
			})
		}
	}
}

func TestInsertedNestedValuesRoundTrip(t *testing.T) {
	values := []any{
		map[string]any{"text": "null", "list": []any{"true", syntax.Number("2"), nil}, "nested": map[string]any{"a: b": "x # y"}},
		[]any{[]any{"one", "true"}, map[string]any{"quoted": "a\nb", "empty": map[string]any{}}, true},
	}
	sources := []string{"old: before\n", "- old\n", "old: {name: before}\n", "[{name: old}]", "old:\n    list:\n        - name: before\n", "old: &base {name: before}\ncopy: *base\n", "[\n  old, # keep\n]\n"}
	for _, src := range sources {
		for _, value := range values {
			t.Run(src, func(t *testing.T) {
				n := parse(t, src)
				key := "new"
				if n.Kind == syntax.Array {
					key = ""
				}
				out := insert(t, src, n, key, value)
				n = parse(t, out)
				var got any
				if n.Kind == syntax.Array {
					got = n.Items[len(n.Items)-1].Interface()
				} else {
					got = n.Get(key).Interface()
				}
				if !reflect.DeepEqual(got, value) {
					t.Fatalf("%q: %#v != %#v", out, got, value)
				}
			})
		}
	}
}
