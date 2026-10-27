package tql_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/formats/toml"
	"github.com/vasileknik76/tql/formats/yaml"
	"github.com/vasileknik76/tql/syntax"
)

func TestQuotedScanBoundaries(t *testing.T) {
	for size := 0; size <= 65; size++ {
		prefix := strings.Repeat("a", size)
		for _, format := range []string{"json", "toml", "yaml"} {
			for _, value := range []string{prefix, prefix + "水😀", prefix + "\n" + prefix, prefix + "\"\\" + prefix, prefix + "\x7f"} {
				literal := syntax.Quote(value)
				if format == "toml" {
					literal = strings.ReplaceAll(literal, "\x7f", `\u007f`)
				}
				if format == "yaml" {
					literal = strings.ReplaceAll(literal, "\x7f", `\u007f`)
				}
				source := `{"value":` + literal + `}`
				if format == "toml" {
					source = "value = " + literal + "\r\n"
				}
				if format == "yaml" {
					source = "value: " + literal + "\r\n"
				}
				doc, err := tql.Parse([]byte(source), format)
				if err != nil {
					t.Fatalf("%s: %v", format, err)
				}
				if got := doc.Value().(map[string]any)["value"]; got != value {
					t.Fatalf("%s size=%d: got %q want %q", format, size, got, value)
				}
			}
		}
		source := "value: \"" + prefix + "   \n  after\"\n"
		doc, err := tql.Parse([]byte(source), "yaml")
		if err != nil {
			t.Fatal(err)
		}
		if got := doc.Value().(map[string]any)["value"]; got != prefix+" after" {
			t.Fatalf("folding size=%d: got %q", size, got)
		}
	}
}

func TestBackendStringOwnership(t *testing.T) {
	for _, tc := range []struct {
		format syntax.Format
		source string
	}{
		{toml.Format{}, "key = \"" + strings.Repeat("x", 65) + "\"\n"},
		{yaml.Format{}, "key: \"" + strings.Repeat("x", 65) + "\"\n"},
	} {
		source := []byte(tc.source)
		root, err := tc.format.Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		want := root.Interface()
		for i := range source {
			source[i] = '!'
		}
		if got := root.Interface(); !reflect.DeepEqual(got, want) {
			t.Fatalf("%T strings alias source bytes", tc.format)
		}
	}
}
