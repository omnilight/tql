package yaml

import (
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestBlockScalarTrailingLines(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"x: >+\n  a\n\n", "a\n\n"},
		{"x: >+\n  a\n\n\n", "a\n\n\n"},
		{"x: >-\n  a\n\n", "a"},
		{"x: >\n  a\n\n", "a\n"},
		{"x: |\n  a\n  ", "a\n"},
		{"x: >\n  a\n  ", "a\n"},
		{"x: |+\n  a\n  ", "a\n\n"},
		{"x: >+\n  a\n  ", "a\n\n"},
		{"x: |\n  a", "a"},
		{"x: >\n  a", "a"},
		{"x: >+\n  a\n\n  b\n", "a\nb\n"},
		{"x: >+\n  a\n\n    b\n", "a\n\n  b\n"},
		{"x: |+\n  \n", "\n"},
		{"x: >+\n  \n", "\n"},
		{"x: |+\n  \n\ny: 1\n", "\n\n"},
		{"x: >+\n  \n\ny: 1\n", "\n\n"},
		{"x: |\n  \n", ""},
		{"x: >\n  \n", ""},
		{"x: |-\n  \n", ""},
		{"x: >-\n  \n", ""},
	} {
		for _, nl := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll(tc.source, "\n", nl)
			t.Run(source, func(t *testing.T) {
				n := parse(t, source).Get("x")
				if n.Value != tc.want {
					t.Fatalf("got %q; want %q", n.Value, tc.want)
				}
			})
		}
	}
}

func TestFoldedScalarMoreIndentedContent(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"v: >\n  \tfoo\n  bar\n", "\tfoo\nbar\n"},
		{"v: >\n  foo\n  \tbar\n", "foo\n\tbar\n"},
		{"v: >\n  foo\n\n  \tbar\n", "foo\n\n\tbar\n"},
		{"v: >\n  \tfoo\n\n  bar\n", "\tfoo\n\nbar\n"},
		{"v: >\n  foo\n    bar\n", "foo\n  bar\n"},
		{"v: >\n  foo\n  bar\n", "foo bar\n"},
		{"v: >\n  foo\n\n  bar\n", "foo\nbar\n"},
	} {
		for _, nl := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll(tc.source, "\n", nl)
			t.Run(source, func(t *testing.T) {
				if got := parse(t, source).Get("v").Value; got != tc.want {
					t.Fatalf("got %q; want %q", got, tc.want)
				}
			})
		}
	}
}

func TestBlockScalarUnicodeWhitespace(t *testing.T) {
	for _, space := range []string{"\u00a0", "\u2003", "\u202f"} {
		for _, style := range []string{"|", ">"} {
			t.Run(style+space, func(t *testing.T) {
				source := "v: " + style + "\n  " + space + "\n    tail\n"
				want := space + "\n  tail\n"
				if got := parse(t, source).Get("v").Value; got != want {
					t.Fatalf("got %q; want %q", got, want)
				}
				for _, invalid := range []string{
					"v: " + style + space + "\n  foo\n",
					"v: " + style + "\n    foo\n  " + space + "\n",
				} {
					if n, err := (Format{}).Parse([]byte(invalid)); err == nil || n != nil {
						t.Errorf("invalid block scalar %q returned %v, %v", invalid, n, err)
					}
				}
			})
		}
	}
}

func TestFoldedScalarReplacementPreservesNeighborBytes(t *testing.T) {
	source := "# heading\r\nv: > # folded\r\n  \tfoo\r\n  bar\r\nkeep: 'untouched' # tail\r\n"
	n := parse(t, source).Get("v")
	value := n.Value.(string) + "!"
	got := replace(t, source, n, value)
	want := "# heading\r\nv: \"\\tfoo\\nbar\\n!\" # folded\r\nkeep: 'untouched' # tail\r\n"
	if got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
	if gotValue := parse(t, got).Get("v").Value; gotValue != value {
		t.Fatalf("got value %q; want %q", gotValue, value)
	}
}

func TestBasePrefixedNumberResolution(t *testing.T) {
	for _, value := range []string{"-0x10", "+0x10", "-0o10", "+0o10", "0x-ff", "0x+ff", "0o-7", "0o+7", "-0x-ff", "+0o+7", "0x", "0o", "0o8", "0xfg", "0x1_2"} {
		n := parse(t, "x: "+value+"\n").Get("x")
		if n.Kind != syntax.String || n.Value != value {
			t.Errorf("%q resolved to %#v (%s)", value, n.Value, n.Kind)
		}
	}
	for _, tc := range []struct{ value, want string }{{"0xff", "255"}, {"0o17", "15"}, {"0xFF", "255"}, {"0o7", "7"}, {"-16", "-16"}, {"+8", "8"}} {
		n := parse(t, "x: "+tc.value+"\n").Get("x")
		if n.Kind != syntax.NumberKind || n.Value != syntax.Number(tc.want) {
			t.Errorf("%q resolved to %#v (%s)", tc.value, n.Value, n.Kind)
		}
	}
}

func TestSignedBaseStringReplacement(t *testing.T) {
	for _, value := range []string{"-0x10", "+0xFF", "-0o10", "+0o7"} {
		t.Run(value, func(t *testing.T) {
			source := "x: 1 # keep\r\ny: 0x10\r\n"
			got := replace(t, source, parse(t, source).Get("x"), value)
			want := "x: \"" + value + "\" # keep\r\ny: 0x10\r\n"
			if got != want {
				t.Fatalf("got %q; want %q", got, want)
			}
			n := parse(t, got)
			if n.Get("x").Kind != syntax.String || n.Get("x").Value != value {
				t.Fatalf("replacement resolved to %#v", n.Get("x").Interface())
			}
			if n.Get("y").Value != syntax.Number("16") {
				t.Fatalf("unsigned control resolved to %#v", n.Get("y").Interface())
			}
		})
	}
}

func TestPlainScalarCannotContinueAfterComment(t *testing.T) {
	for _, source := range []string{
		"x: a # comment\n  b\n",
		"x: a\n # comment\n  b\n",
		"x: a\n  b # comment\n  c\n",
	} {
		if _, err := (Format{}).Parse([]byte(source)); err == nil {
			t.Errorf("accepted continuation after comment: %q", source)
		}
	}
	for _, source := range []string{
		"x: a # comment\ny: b\n",
		"x: a\n # comment\ny: b\n",
		"x: a\n  b # comment\ny: c\n",
	} {
		parse(t, source)
	}
}

func TestCommentsMayHaveTabSeparation(t *testing.T) {
	for _, source := range []string{"true\n\t# keep", "true\n \t # keep\n", "\t# heading\ntrue\n", " \t # heading\ntrue\n"} {
		if got := parse(t, source).Interface(); got != true {
			t.Errorf("%q resolved to %#v", source, got)
		}
	}
	source := "0\n0\t# keep"
	root := parse(t, source)
	if out := replace(t, source, root, true); out != "true\n\t# keep" {
		t.Fatalf("comment formatting changed: %q", out)
	} else if got := parse(t, out).Interface(); got != true {
		t.Fatalf("replacement resolved to %#v", got)
	}
}
