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
		{"x: |+\n  a\n  ", "a\n"},
		{"x: >+\n  a\n  ", "a\n"},
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

func TestBasePrefixedNumberResolution(t *testing.T) {
	for _, value := range []string{"0x-ff", "0x+ff", "0o-7", "0o+7", "-0x-ff", "+0o+7", "0x", "0o", "0o8", "0xfg", "0x1_2"} {
		n := parse(t, "x: "+value+"\n").Get("x")
		if n.Kind != syntax.String || n.Value != value {
			t.Errorf("%q resolved to %#v (%s)", value, n.Value, n.Kind)
		}
	}
	for _, tc := range []struct{ value, want string }{{"0xff", "255"}, {"0o17", "15"}, {"-0xFF", "-255"}, {"+0o7", "7"}} {
		n := parse(t, "x: "+tc.value+"\n").Get("x")
		if n.Kind != syntax.NumberKind || n.Value != syntax.Number(tc.want) {
			t.Errorf("%q resolved to %#v (%s)", tc.value, n.Value, n.Kind)
		}
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
