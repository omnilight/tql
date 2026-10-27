package toml_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestTOML11Values(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         any
	}{
		{"basic escapes", `value = "\x00\x7f\xFF\e"`, map[string]any{"value": "\x00\x7fÿ\x1b"}},
		{"multiline escapes", "value = \"\"\"\n\\xE9\\e\"\"\"\n", map[string]any{"value": "é\x1b"}},
		{"two-digit hex escape", `value = "\x000"`, map[string]any{"value": "\x000"}},
		{"escaped key", `"\x41\e" = 1`, map[string]any{"A\x1b": tql.Number("1")}},
		{"local time", "value = 13:37\n", map[string]any{"value": tql.Temporal("13:37")}},
		{"local datetime", "value = 1979-05-27T07:32\n", map[string]any{"value": tql.Temporal("1979-05-27T07:32")}},
		{"offset datetime", "value = 1979-05-27 07:32-07:00\n", map[string]any{"value": tql.Temporal("1979-05-27 07:32-07:00")}},
		{"UTC datetime", "value = 1979-05-27t07:32z\n", map[string]any{"value": tql.Temporal("1979-05-27t07:32z")}},
		{"inline trivia", "value = {# open\n a = 1, # first\n b = { c = [2,], },\n} # end\n", map[string]any{"value": map[string]any{"a": tql.Number("1"), "b": map[string]any{"c": []any{tql.Number("2")}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := tql.Parse([]byte(tc.source), "toml")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(doc.Value(), tc.want) {
				t.Fatalf("got %#v; want %#v", doc.Value(), tc.want)
			}
			for range 2 {
				result, err := doc.Exec(". = .")
				if err != nil || result.Changed || !bytes.Equal(result.Bytes, []byte(tc.source)) {
					t.Fatalf("no-op: %#v, %v", result, err)
				}
			}
		})
	}
}

func TestTOML11InvalidValues(t *testing.T) {
	for _, source := range []string{
		`value="\x0"`, `value="\xFG"`, `value="\x000" "next"`,
		`value="\uD800"`, `value="\U00110000"`,
		"value=24:00", "value=13:60", "value=13:37.5", "value=1979-05-27T07:32+24:00",
		"value={a\n=1}", "value={a=\n1}", "value={a=# invalid\n1}", "value={,}", "value={a=1,,}",
		"value={a=1, a=2,}", "a=1\n\"\\x61\"=2", "value={a=1\r}",
	} {
		t.Run(source, func(t *testing.T) {
			if doc, err := tql.Parse([]byte(source), "toml"); err == nil || doc != nil {
				t.Fatalf("accepted invalid input: %v, %v", doc, err)
			}
		})
	}
}

func TestTOML11InlineEdits(t *testing.T) {
	for _, tc := range []struct{ source, query, want string }{
		{"x={a=1,}\n", `x.b=2`, "x={a=1,b=2,}\n"},
		{"x={a=1,}\n", `del(x.a)`, "x={}\n"},
		{"x={a.b=1,a.c=2,}\n", `del(x.a)`, "x={}\n"},
		{"x={a=1, b=2,}\n", `del(x.b)`, "x={a=1, }\n"},
		{"x={ # open\n}\n", `x.b=2`, "x={ # open\n  b=2\n}\n"},
		{"x={\n  a=1 # keep\n}\n", `x.b=2`, "x={\n  a=1, # keep\n  b=2\n}\n"},
		{"x={\n  a=1, # keep\n}\n", `x.b=2`, "x={\n  a=1, # keep\n  b=2,\n}\n"},
		{"x={\n  a=1, # keep\n}\n", `del(x.a)`, "x={\n   # keep\n}\n"},
		{"x={\n  a={b=1}, # keep\n}\n", `x.a.c=2`, "x={\n  a={b=1,c=2}, # keep\n}\n"},
		{"x={\n  a.b=1, # keep\n}\n", `x.a.c=2`, "x={\n  a.b=1, # keep\n  a.c=2,\n}\n"},
		{"short=13:37\nkeep=0xFf # keep\n", `copy=short`, "short=13:37\nkeep=0xFf # keep\ncopy=13:37\n"},
	} {
		for _, nl := range []string{"\n", "\r\n"} {
			source, want := strings.ReplaceAll(tc.source, "\n", nl), strings.ReplaceAll(tc.want, "\n", nl)
			t.Run(source+tc.query, func(t *testing.T) {
				doc, err := tql.Parse([]byte(source), "toml")
				if err != nil {
					t.Fatal(err)
				}
				result, err := doc.Exec(tc.query)
				if err != nil {
					t.Fatal(err)
				}
				if string(result.Bytes) != want {
					t.Fatalf("got %q; want %q", result.Bytes, want)
				}
				for range 2 {
					result, err = doc.Exec(tc.query)
					if err != nil || result.Changed || string(result.Bytes) != want {
						t.Fatalf("repeated edit: %#v, %v", result, err)
					}
				}
				before := doc.Bytes()
				if result, err := doc.Exec(`keep=1; 1 / 0`); err == nil || result != nil {
					t.Fatal("expected rollback error")
				}
				if !bytes.Equal(before, doc.Bytes()) {
					t.Fatal("failed transaction changed bytes")
				}
			})
		}
	}
}
