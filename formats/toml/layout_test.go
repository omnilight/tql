package toml

import (
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

// All source documents in this file are invented, independent of external files.
func TestAssignmentBeforeSectionTail(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"blank separator", "[palette]\nbase='red'\n\n[canvas]\nwidth=8\n", "[palette]\nbase='red'\naccent='blue'\n\n[canvas]\nwidth=8\n"},
		{"intentional blank lines", "[palette]\nbase='red'\n\n \t\n\n[canvas]\nwidth=8\n", "[palette]\nbase='red'\naccent='blue'\n\n \t\n\n[canvas]\nwidth=8\n"},
		{"comment boundary", "[palette]\nbase='red' # original\n\n# Canvas dimensions.\n  [canvas]\nwidth=8\n", "[palette]\nbase='red' # original\naccent='blue'\n\n# Canvas dimensions.\n  [canvas]\nwidth=8\n"},
		{"trailing comment", "[palette]\nbase='red'\n# Leave this note in place.\n\n", "[palette]\nbase='red'\naccent='blue'\n# Leave this note in place.\n\n"},
		{"empty table", "[palette] # Colors.\n\n[canvas]\nwidth=8\n", "[palette] # Colors.\naccent = \"blue\"\n\n[canvas]\nwidth=8\n"},
		{"empty table preamble", "[palette]\n# Colors.\n\n[canvas]\nwidth=8\n", "[palette]\n# Colors.\naccent = \"blue\"\n\n[canvas]\nwidth=8\n"},
		{"no final newline", "[palette]\nbase='red'", "[palette]\nbase='red'\naccent='blue'"},
		{"multiline last value", "[palette]\nbase=[\n  'red',\n]\n\n[canvas]\nwidth=8\n", "[palette]\nbase=[\n  'red',\n]\naccent=\"blue\"\n\n[canvas]\nwidth=8\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, nl := range []string{"\n", "\r\n"} {
				source := strings.ReplaceAll(tc.source, "\n", nl)
				root := parse(t, source)
				edits, err := (Format{}).Insert([]byte(source), root, root.Get("palette"), "accent", "blue")
				got := apply(t, source, edits, err)
				if want := strings.ReplaceAll(tc.want, "\n", nl); got != want {
					t.Fatalf("got %q\nwant %q", got, want)
				}
			}
		})
	}
}

func TestNewScalarArrayWidth(t *testing.T) {
	values := []any{strings.Repeat("m", 36), strings.Repeat("n", 36), strings.Repeat("o", 36)}
	inline := "[\"" + values[0].(string) + "\", \"" + values[1].(string) + "\", \"" + values[2].(string) + "\"]"
	block := "[\n  \"" + values[0].(string) + "\",\n  \"" + values[1].(string) + "\",\n  \"" + values[2].(string) + "\"\n]"
	for _, tc := range []struct {
		name, source, parent, want string
		style                      syntax.Style
	}{
		{name: "no layout evidence", source: "count=3\n", want: "count=3\nwords=" + block + "\n"},
		{name: "explicit flow", source: "count=3\n", style: syntax.Style{Arrays: "flow"}, want: "count=3\nwords=" + inline + "\n"},
		{name: "inline table", source: "box={count=3}\n", parent: "box", want: "box={count=3,words=" + strings.ReplaceAll(inline, ", ", ",") + "}\n"},
		{name: "short inline evidence", source: "sample=[1, 2]\n", want: "sample=[1, 2]\nwords=" + block + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, nl := range []string{"\n", "\r\n"} {
				source := strings.ReplaceAll(tc.source, "\n", nl)
				root := parse(t, source)
				parent := root
				if tc.parent != "" {
					parent = root.Get(tc.parent)
				}
				edits, err := (Format{style: tc.style}).Insert([]byte(source), root, parent, "words", values)
				got := apply(t, source, edits, err)
				if want := strings.ReplaceAll(tc.want, "\n", nl); got != want {
					t.Fatalf("got %q\nwant %q", got, want)
				}
			}
		})
	}
}

func TestAdjacentTableArrayTemplate(t *testing.T) {
	const source = "[[tiles]]\nlabel='circle'\n\n[[tiles]]\nshades=[\n    'red',\n]\n"
	const want = "[[tiles]]\nlabel='circle'\ncolors=[\n    'blue',\n    'gold',\n]\n\n[[tiles]]\nshades=[\n    'red',\n]\n"
	for _, nl := range []string{"\n", "\r\n"} {
		src := strings.ReplaceAll(source, "\n", nl)
		root := parse(t, src)
		edits, err := (Format{}).Insert([]byte(src), root, root.Get("tiles").Items[0], "colors", []any{"blue", "gold"})
		if got := apply(t, src, edits, err); got != strings.ReplaceAll(want, "\n", nl) {
			t.Fatalf("unexpected layout: %q", got)
		}
	}
}

func TestArrayWidthKeepsExistingLayoutAndScalars(t *testing.T) {
	long := strings.Repeat("z", 140)
	t.Run("one scalar", func(t *testing.T) {
		const source = "count=3\n"
		root := parse(t, source)
		edits, err := (Format{}).Insert([]byte(source), root, root, "word", []any{long})
		if got := apply(t, source, edits, err); got != source+"word=[\""+long+"\"]\n" {
			t.Fatal(got)
		}
	})
	t.Run("existing inline array", func(t *testing.T) {
		const source = "words=[\"a\", \"b\"]\n"
		root := parse(t, source)
		edits, err := (Format{}).Replace([]byte(source), root, root.Get("words"), []any{long, long})
		if got := apply(t, source, edits, err); got != "words=[\""+long+"\", \""+long+"\"]\n" {
			t.Fatal(got)
		}
	})
}
