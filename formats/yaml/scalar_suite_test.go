package yaml_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestQuotedScalarEscapedWhitespaceFolding(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"\"one\\t\n  two\"", "one\t two"},
		{"\"one\\t  \n  two\"", "one\t two"},
		{"\"one\\\t\n  two\"", "one\t two"},
		{"\"one\\\t  \n  two\"", "one\t two"},
		{"\"one\\ \n  two\"", "one  two"},
		{"\"one\\x09 \n  two\"", "one\t two"},
		{"\"one\\u0020 \n  two\"", "one  two"},
		{"\"one \t\n  two\"", "one two"},
		{"'one \t\n  two'", "one two"},
	} {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll("value: "+tc.source+" # keep\nother: 0x10\n", "\n", newline)
			t.Run(source, func(t *testing.T) {
				doc, err := tql.Parse([]byte(source), "yaml")
				if err != nil {
					t.Fatal(err)
				}
				if got := doc.Value().(map[string]any)["value"]; got != tc.want {
					t.Fatalf("got %q; want %q", got, tc.want)
				}
				result, err := doc.Exec(`value = value`)
				if err != nil || result.Changed || !bytes.Equal(result.Bytes, []byte(source)) {
					t.Fatalf("no-op: %#v, %v", result, err)
				}
				result, err = doc.Exec(`value |= . + "!"`)
				if err != nil {
					t.Fatal(err)
				}
				if got := doc.Value().(map[string]any)["value"]; got != tc.want+"!" {
					t.Fatalf("edited value = %q", got)
				}
				if !bytes.HasSuffix(result.Bytes, []byte(" # keep"+newline+"other: 0x10"+newline)) {
					t.Fatalf("neighbor bytes changed: %q", result.Bytes)
				}
				before := doc.Bytes()
				result, err = doc.Exec(`value = value`)
				if err != nil || result.Changed || !bytes.Equal(result.Bytes, before) {
					t.Fatalf("repeat: %#v, %v", result, err)
				}
			})
		}
	}
}

func TestBlockScalarContentBoundaries(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   any
	}{
		{"--- >\nline1\n# no comment\nline3\n", "line1 # no comment line3\n"},
		{"|2\n  x\n", "x\n"},
		{"  |2\n    x\n", "  x\n"},
		{"|\n \n  \n  literal\n   \n  \n  text\n\n # Comment\n", "\n\nliteral\n \n\ntext\n"},
		{"strip: |-\n  # text\n  \n # Clip\n  # comments:\n\nclip: |\n  # text\n \n # Keep\n  # comments:\n\nkeep: |+\n  # text\n\n # Trail\n  # comments.\n", map[string]any{"strip": "# text", "clip": "# text\n", "keep": "# text\n\n"}},
		{"- |+\n   ", []any{"\n"}},
		{"foo: |\n  x\n   ", map[string]any{"foo": "x\n \n"}},
		{"value: |\n  \t\nother: 1\n", map[string]any{"value": "\t\n", "other": tql.Number("1")}},
	} {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll(tc.source, "\n", newline)
			t.Run(source, func(t *testing.T) {
				doc, err := tql.Parse([]byte(source), "yaml")
				if err != nil {
					t.Fatal(err)
				}
				if got := doc.Value(); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("got %#v; want %#v", got, tc.want)
				}
				for range 2 {
					result, err := doc.Exec(`. = .`)
					if err != nil || result.Changed || !bytes.Equal(result.Bytes, []byte(source)) {
						t.Fatalf("no-op: %#v, %v", result, err)
					}
				}
			})
		}
	}
	for _, source := range []string{"value: |\n\t\nother: 1\n", "value: |2\n \t\nother: 1\n"} {
		if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
			t.Fatalf("accepted tab indentation: %q", source)
		}
	}
}

func TestBlockScalarTrailingCommentsSurviveEdit(t *testing.T) {
	source := "value: | # header\r\n  before\r\n # trailing\r\n  # more\r\nother: 0x10\r\n"
	doc, err := tql.Parse([]byte(source), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	result, err := doc.Exec(`value = "after"`)
	if err != nil {
		t.Fatal(err)
	}
	want := "value: \"after\" # header\r\n # trailing\r\n  # more\r\nother: 0x10\r\n"
	if !bytes.Equal(result.Bytes, []byte(want)) {
		t.Fatalf("got %q; want %q", result.Bytes, want)
	}
	if got := doc.Value().(map[string]any)["value"]; got != "after" {
		t.Fatalf("value = %#v", got)
	}
}

func TestQuotedScalarDocumentMarkers(t *testing.T) {
	for _, source := range []string{
		"\"a\n---\nb\"\n",
		"'a\n...\nb'\n",
		"\"a\\\n---\nb\"\n",
		"\"a\\\n... x\nb\"\n",
	} {
		if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
			t.Fatalf("accepted document marker in quoted scalar: %q", source)
		}
	}
	for _, tc := range []struct{ source, want string }{
		{"\"a\n  ---\n  b\"\n", "a --- b"},
		{"'a\n  ...\n  b'\n", "a ... b"},
		{"\"a\\n--- b\"\n", "a\n--- b"},
	} {
		doc, err := tql.Parse([]byte(tc.source), "yaml")
		if err != nil {
			t.Fatal(err)
		}
		if got := doc.Value(); got != tc.want {
			t.Fatalf("got %#v; want %q", got, tc.want)
		}
		result, err := doc.Exec(`. = .`)
		if err != nil || result.Changed || string(result.Bytes) != tc.source {
			t.Fatalf("no-op: %#v, %v", result, err)
		}
	}
}
