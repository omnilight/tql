package tql_test

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/syntax"
)

// Inferred quotes and collection layouts must never change a new value's type
// or text. Exercise the public transaction boundary as well as each backend.
func FuzzStyledInsertion(f *testing.F) {
	for i, value := range []string{
		"true", "12", "null", "a: b", "a # b", "can't", "line\nbreak",
		"é🌳", "\t\r\n", "\x00\x7f\u0080\u0085\ufffe\uffff", "---", "[a,b]",
	} {
		f.Add(uint8(i), value)
	}
	f.Fuzz(func(t *testing.T, seed uint8, value string) {
		if len(value) > 512 || !utf8.ValidString(value) {
			t.Skip()
		}
		cases := []struct {
			format  string
			sources []string
		}{
			{"json", []string{`{}`, `{ "old" : [ "before" ] }`, "{\r\n\t\"old\" : [{\"value\":\"before\"}]\r\n}"}},
			{"toml", []string{"", "old=['before'] # keep\n", "[section]\r\nold = { value = 'before' }\r\n"}},
			{"yaml", []string{"{}", "old:\n    - value: before # keep\n", "'old': [ 'before' ]\r\n"}},
		}
		for _, tc := range cases {
			t.Run(tc.format, func(t *testing.T) {
				source := []byte(tc.sources[int(seed)%len(tc.sources)])
				doc, err := tql.Parse(source, tc.format)
				if err != nil {
					t.Fatal(err)
				}
				program := `added = [` + syntax.Quote(value) + `, {value: ` + syntax.Quote(value) + `}, [` + syntax.Quote(value) + `]]`
				result, err := doc.Exec(program)
				if err != nil {
					t.Fatalf("representable value %q failed: %v", value, err)
				}
				assertQuery(t, doc, `added`, []any{value, map[string]any{"value": value}, []any{value}})
				if !bytes.Equal(source, []byte(tc.sources[int(seed)%len(tc.sources)])) {
					t.Fatal("insertion mutated caller's source")
				}
				if !bytes.Equal(result.Bytes, doc.Bytes()) {
					t.Fatal("document and execution result disagree")
				}
			})
		}
	})
}
