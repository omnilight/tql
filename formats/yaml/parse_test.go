package yaml_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/syntax"
)

func TestUnicodeSpacesArePlainScalarContent(t *testing.T) {
	for _, space := range []string{"\u00a0", "\u2003", "\u202f", "\u3000"} {
		for _, tc := range []struct {
			source string
			want   any
		}{
			{space + "\n", space},
			{"v: foo\n  " + space + "\n  bar\n", map[string]any{"v": "foo " + space + " bar"}},
			{"v: " + space + "\n", map[string]any{"v": space}},
		} {
			t.Run(tc.source, func(t *testing.T) {
				d, err := tql.Parse([]byte(tc.source), "yaml")
				if err != nil {
					t.Fatal(err)
				}
				if got := d.Value(); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("value %#v != %#v", got, tc.want)
				}
				for range 2 {
					r, err := d.Exec(`. = .`)
					if err != nil {
						t.Fatal(err)
					}
					if r.Changed || string(r.Bytes) != tc.source {
						t.Fatalf("no-op changed source: %q", r.Bytes)
					}
				}
			})
		}
	}
	for _, source := range []string{" \t\n", "# comment\n \t\n"} {
		d, err := tql.Parse([]byte(source), "yaml")
		if err != nil || d.Value() != nil {
			t.Fatalf("ASCII blank document %q: %v, %v", source, d, err)
		}
	}
}

func TestInlineDocumentStart(t *testing.T) {
	for _, tc := range []struct {
		source, query, want string
		value               any
	}{
		{"--- foo # keep\n... # end\n", `. |= . + "!"`, "--- \"foo!\" # keep\n... # end\n", "foo"},
		{"--- [1, 2] # keep\n", `.[1] = 3`, "--- [1, 3] # keep\n", []any{syntax.Number("1"), syntax.Number("2")}},
		{"--- {a: 1, b: 2} # keep\n", `a = 3`, "--- {a: 3, b: 2} # keep\n", map[string]any{"a": syntax.Number("1"), "b": syntax.Number("2")}},
		{"%YAML 1.2\n---\tfoo\n", `. = "bar"`, "%YAML 1.2\n---\t\"bar\"\n", "foo"},
		{"--- foo\nbar\n", `. = "after"`, "--- \"after\"\n\n", "foo bar"},
		{"\ufeff--- foo\n", `. = "bar"`, "\ufeff--- \"bar\"\n", "foo"},
	} {
		for _, nl := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(tc.source, "\n", nl)
			want := strings.ReplaceAll(tc.want, "\n", nl)
			t.Run(source, func(t *testing.T) {
				d, err := tql.Parse([]byte(source), "yaml")
				if err != nil {
					t.Fatal(err)
				}
				if got := d.Value(); !reflect.DeepEqual(got, tc.value) {
					t.Fatalf("value %#v != %#v", got, tc.value)
				}
				if r, err := d.Exec(`. = .`); err != nil || r.Changed || string(r.Bytes) != source {
					t.Fatalf("no-op: %v, %v", r, err)
				}
				r, err := d.Exec(tc.query)
				if err != nil {
					t.Fatal(err)
				}
				if string(r.Bytes) != want {
					t.Fatalf("edit %q != %q", r.Bytes, want)
				}
				if r, err := d.Exec(`. = .`); err != nil || r.Changed || string(r.Bytes) != want {
					t.Fatalf("post-edit no-op: %v, %v", r, err)
				}
			})
		}
	}
	for _, source := range []string{"---foo\n", "---\u00a0\n"} {
		d, err := tql.Parse([]byte(source), "yaml")
		if err != nil || d.Value() != strings.TrimSuffix(source, "\n") {
			t.Fatalf("non-marker %q: %v, %v", source, d, err)
		}
	}
	for _, source := range []string{"--- a: b\n", "--- - a\n", "--- [1]\n--- [2]\n", "--- foo\n--- bar\n"} {
		if d, err := tql.Parse([]byte(source), "yaml"); err == nil || d != nil {
			t.Fatalf("accepted invalid inline document %q", source)
		}
	}
}

func TestDeletionKeepsLineExtents(t *testing.T) {
	for _, tc := range []struct{ source, query, want string }{
		{"a: 1 # keep\nb: 2\nc: 3\n", `del(a)`, "# keep\nb: 2\nc: 3\n"},
		{"a: 1\nb: 2\nc: 3", `del(b)`, "a: 1\nc: 3"},
		{"a: 1\nb: 2\nc: 3", `del(c)`, "a: 1\nb: 2\n"},
		{"a: [1,\n  2]\nb: 3\n", `del(a)`, "b: 3\n"},
		{"a: |\n  text\nb: 3\n", `del(a)`, "b: 3\n"},
		{"- 1 # keep\n- 2\n- 3\n", `del(.[0])`, "# keep\n- 2\n- 3\n"},
		{"- 1\n- 2\n- 3", `del(.[1])`, "- 1\n- 3"},
		{"- 1\n- 2\n- 3", `del(.[2])`, "- 1\n- 2\n"},
		{"- [1,\n  2]\n- 3\n", `del(.[0])`, "- 3\n"},
	} {
		for _, nl := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll(tc.source, "\n", nl)
			want := strings.ReplaceAll(tc.want, "\n", nl)
			t.Run(source+tc.query, func(t *testing.T) {
				r, err := tql.Eval([]byte(source), "yaml", tc.query)
				if err != nil {
					t.Fatal(err)
				}
				if string(r.Bytes) != want {
					t.Fatalf("delete %q != %q", r.Bytes, want)
				}
			})
		}
	}
}

func TestBlockPropertyNestingLimit(t *testing.T) {
	for _, count := range []int{1, 257} {
		source := strings.Repeat("!!str\n", count) + "value\n"
		doc, err := tql.Parse([]byte(source), "yaml")
		if count > 256 {
			if err == nil || doc != nil || !strings.Contains(err.Error(), "property nesting limit exceeded") {
				t.Fatalf("expected property limit: %v, %v", doc, err)
			}
		} else if err != nil || doc.Value() != "value" {
			t.Fatalf("valid property: %v, %v", doc, err)
		}
	}
	var source strings.Builder
	for i := range 256 {
		indent := strings.Repeat("  ", i)
		source.WriteString(indent + "!!map\n" + indent + "k:\n")
	}
	source.WriteString(strings.Repeat("  ", 256) + "0\n")
	doc, err := tql.Parse([]byte(source.String()), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	value := doc.Value()
	for range 256 {
		value = value.(map[string]any)["k"]
	}
	if value != tql.Number("0") {
		t.Fatalf("leaf = %#v", value)
	}
}
