package yaml

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestAnchorsAndAliases(t *testing.T) {
	src := "base: &defaults\n  name: foo\n  enabled: true\ncopy: *defaults # alias\n"
	n := parse(t, src)
	if !reflect.DeepEqual(n.Get("base").Interface(), n.Get("copy").Interface()) {
		t.Fatal(n.Interface())
	}
	out := replace(t, src, n.Get("copy").Get("name"), "bar")
	want := "base: &defaults\n  name: foo\n  enabled: true\ncopy: {\"enabled\":true,\"name\":\"bar\"} # alias\n"
	if out != want {
		t.Fatalf("%q", out)
	}
	out = insert(t, src, n.Get("copy"), "port", syntax.Number("8080"))
	got := parse(t, out)
	if got.Get("copy").Get("port").Value != syntax.Number("8080") || got.Get("base").Get("port") != nil {
		t.Fatal(out)
	}
	out = remove(t, src, n.Get("copy").Get("enabled"))
	got = parse(t, out)
	if got.Get("copy").Get("enabled") != nil || got.Get("base").Get("enabled") == nil {
		t.Fatal(out)
	}
}
func FuzzAliasEdits(f *testing.F) {
	f.Add("foo")
	f.Add("\n\"'\t\x00")
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 1000 {
			return
		}
		value = strings.ToValidUTF8(value, "�")
		quoted, e := encode(value)
		if e != nil {
			t.Fatal(e)
		}
		src := "base: &a [{name: " + quoted + "}, 2]\ncopy: *a # c\n"
		n := parse(t, src)
		out := replace(t, src, n.Get("copy").Items[0].Get("name"), "changed")
		changed := parse(t, out)
		if changed.Get("base").Items[0].Get("name").Value != value || changed.Get("copy").Items[0].Get("name").Value != "changed" {
			t.Fatal(out)
		}
		out = remove(t, src, n.Get("copy").Items[0])
		if len(parse(t, out).Get("copy").Items) != 1 {
			t.Fatal(out)
		}
		out = insert(t, src, n.Get("copy"), "", true)
		if len(parse(t, out).Get("copy").Items) != 3 {
			t.Fatal(out)
		}
	})
}
func TestAliasArrayMutations(t *testing.T) {
	src := "base: &a [{name: foo}, 2]\ncopy: *a\n"
	n := parse(t, src)
	out := replace(t, src, n.Get("copy").Items[0].Get("name"), "bar")
	if parse(t, out).Get("copy").Items[0].Get("name").Value != "bar" {
		t.Fatal(out)
	}
	out = remove(t, src, n.Get("copy").Items[0])
	got := parse(t, out)
	if len(got.Get("copy").Items) != 1 || len(got.Get("base").Items) != 2 {
		t.Fatal(out)
	}
	out = insert(t, src, n.Get("copy"), "", true)
	if len(parse(t, out).Get("copy").Items) != 3 {
		t.Fatal(out)
	}
}
func TestAnchorEditReparsesAlias(t *testing.T) {
	src := "a: &x 1\nb: *x\n"
	n := parse(t, src)
	out := replace(t, src, n.Get("a"), syntax.Number("2"))
	if out != "a: &x 2\nb: *x\n" {
		t.Fatal(out)
	}
	if parse(t, out).Get("b").Value != syntax.Number("2") {
		t.Fatal(out)
	}
}
func TestAnchorForms(t *testing.T) {
	for _, src := range []string{"[&a 1, *a]", "a: &x null\nb: *x\n", "a: &x\n  - 1\n  - 2\nb: *x\n", "- &x\n  a: 1\n- *x\n", "&x\na: 1\n", "a: &x |\n  hello\nb: *x\n"} {
		parse(t, src)
	}
	src := "[&a 1, 2]"
	n := parse(t, src)
	out := remove(t, src, n.Items[0])
	if len(parse(t, out).Items) != 1 {
		t.Fatal(out)
	}
}
func TestAliasFailures(t *testing.T) {
	for _, src := range []string{"a: *unknown", "a: &x [*x]", "a: *later\nb: &later 1", "a: &x 1\nb: &x 2", "a: & 1"} {
		if _, e := (Format{}).Parse([]byte(src)); e == nil {
			t.Fatalf("accepted %q", src)
		}
	}
}
