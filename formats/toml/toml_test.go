package toml

import (
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func parse(t *testing.T, src string) *syntax.Node {
	t.Helper()
	n, e := (Format{}).Parse([]byte(src))
	if e != nil {
		t.Fatal(e)
	}
	return n
}
func apply(t *testing.T, src string, edits []syntax.Edit, err error) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	b, e := syntax.Apply([]byte(src), edits)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = (Format{}).Parse(b); e != nil {
		t.Fatalf("invalid edited TOML: %v\n%s", e, b)
	}
	return string(b)
}
func TestSpecValues(t *testing.T) {
	src := `# sample
 title = "TOML Example"
 "quoted.dot" . 'key' = '\raw\'
 ints = [0, +99, -17, 1_000, 0xDEAD_BEEF, 0o755, 0b1101]
 floats = [1.0, -0.0, 5e+22, 6.626e-34, 1_000.1_2, inf, -inf, nan]
 when = 1979-05-27T07:32:00.999999999999Z
 local = 1979-05-27 07:32:00
 day = 1979-05-27
 time = 07:32:00.999
 multiline = """
 Roses are red
 Violets are blue"""
 literal = '''
 The first newline is
 trimmed in raw strings.
 All other whitespace
 is preserved.
 '''
 continued = """The quick brown \
     fox jumps over \
       the lazy dog."""
 escaped = "\u03B4\U0001F600\t\b\n\r\f\"\\"
 inline = {first = "Tom", last = "Preston-Werner", point.x = 1, point.y = 2}
 arr = [
   1, # comment
   "two",
   {a = 3},
 ]
 [owner]
 name = "Tom"
 [database.settings]
 enabled = true
 [database]
 ports = [8000, 8001, 8002]
 [[products]]
 name = "Hammer"
 [products.details]
 weight = 10
 [[products]]
 name = "Nail"
 [[products.reviews]]
 score = 5
 `
	n := parse(t, src)
	if n.Get("continued").Value != "The quick brown fox jumps over the lazy dog." {
		t.Fatal(n.Get("continued").Value)
	}
	if n.Get("quoted.dot").Get("key").Value != `\raw\` {
		t.Fatal("quoted key")
	}
	if n.Get("products").Items[0].Get("details").Get("weight").Value != syntax.Number("10") {
		t.Fatal("array-table path")
	}
	if n.Get("ints").Items[4].Value != syntax.Number("3735928559") {
		t.Fatal("hex")
	}
}
func TestInvalid(t *testing.T) {
	cases := []string{
		"x=01", "x=+0x12", "x=1_", "x=1__2", "x=.7", "x=7.", "x=0e", "x=9223372036854775808", "x=0xffffffffffffffff", "x=null", "x=TRUE", "x=bare", "x=2023-02-29", "x=25:00:00", "x=1979-05-27T07:32:00+99:00", "x=1 x=2", "x='unterminated", "x=\"a\nb\"", "x=\"\\uD800\"", "x=\"\\U00110000\"", "x=\"\\u+123\"", "x=[1 2]", "x=[1,,2]", "x={a=1,a=2}", "x=1\nx=2", "a.b=1\n[a]", "a={b=1}\n[a.c]", "[a]\n[a]", "a=1\n[a.b]", "a=[]\n[[a]]", "x='\x7f'", "#\x01", "x=1\r", "\xef\xbb\xbfx=1", "x=\"\"\"abc\"\"\"\"\"\"", "'''key'''=1",
	}
	for _, src := range cases {
		t.Run(strings.ReplaceAll(src, "\n", "/"), func(t *testing.T) {
			if _, err := (Format{}).Parse([]byte(src)); err == nil {
				t.Fatalf("accepted invalid TOML %q", src)
			}
		})
	}
}
func TestStringQuotes(t *testing.T) {
	cases := []struct{ source, want string }{
		{`x = """a""""`, "a\""},
		{`x = """a"""""`, "a\"\""},
		{`x = '''a''''`, "a'"},
		{`x = '''a'''''`, "a''"},
	}
	for _, tc := range cases {
		n := parse(t, tc.source)
		if n.Get("x").Value != tc.want {
			t.Fatalf("got %q want %q", n.Get("x").Value, tc.want)
		}
	}
}
func TestReplacePreservesEveryOtherByte(t *testing.T) {
	src := "# top\r\n[ app ] # table\r\n  answer  =  0xFf # exact\r\narray = [ 1, # one\r\n2, ]\r\n"
	n := parse(t, src)
	edits, e := (Format{}).Replace([]byte(src), n, n.Get("app").Get("answer"), syntax.Number("3"))
	got := apply(t, src, edits, e)
	want := strings.Replace(src, "0xFf", "3", 1)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
func TestInsertSections(t *testing.T) {
	cases := []struct {
		src, path, key string
		want           string
	}{
		{"# top\nx=1\n[a]\ny=2\n", "", "new", "# top\nx=1\nnew=3\n[a]\ny=2\n"},
		{"[a]\nx=1\n[b]\ny=2\n", "a", "new", "[a]\nx=1\nnew=3\n[b]\ny=2\n"},
		{"a.b.x=1\n", "a.b", "new", "a.b.x=1\na.b.new=3\n"},
		{"x={ a=1 }\n", "x", "new", "x={ a=1, new=3 }\n"},
		{"x={ a.b=1 }\n", "x.a", "new", "x={ a.b=1, a.new=3 }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			n := parse(t, tc.src)
			parent := n
			if tc.path != "" {
				for _, k := range strings.Split(tc.path, ".") {
					parent = parent.Get(k)
				}
			}
			edits, e := (Format{}).Insert([]byte(tc.src), n, parent, tc.key, syntax.Number("3"))
			got := apply(t, tc.src, edits, e)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
func TestDelete(t *testing.T) {
	cases := []struct {
		src, path string
		index     int
		want      string
	}{
		{"# top\na.b = 1 # keep\na.c=2\n", "a.b", -1, "# top\n # keep\na.c=2\n"},
		{"x=[ 1, # keep\n 2, 3, ]\n", "x", 1, "x=[ 1, # keep\n 3, ]\n"},
		{"x=[1, 2]\n", "x", 1, "x=[1]\n"},
		{"x={a=1, b=2, c=3}\n", "x.b", -1, "x={a=1, c=3}\n"},
		{"x={a.b=1, a.c=2, z=3}\n", "x.a", -1, "x={z=3}\n"},
		{"x={z=3, a.b=1, a.c=2}\n", "x.a", -1, "x={z=3}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			n := parse(t, tc.src)
			target := n
			for _, k := range strings.Split(tc.path, ".") {
				target = target.Get(k)
			}
			if tc.index >= 0 {
				target = target.Items[tc.index]
			}
			edits, e := (Format{}).Delete([]byte(tc.src), n, target)
			got := apply(t, tc.src, edits, e)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
func TestReplaceTablePreservesComments(t *testing.T) {
	src := "# top\n[a] # header\nx=1 # x\n[a.child]\ny=2\n[b]\nz=3\n"
	n := parse(t, src)
	edits, e := (Format{}).Replace([]byte(src), n, n.Get("a"), []any{syntax.Number("1"), syntax.Number("2")})
	got := apply(t, src, edits, e)
	for _, comment := range []string{"# top", "# header", "# x"} {
		if !strings.Contains(got, comment) {
			t.Fatal("lost comment", got)
		}
	}
	if parse(t, got).Get("a").Kind != syntax.Array {
		t.Fatal(got)
	}
}
func TestAppend(t *testing.T) {
	for _, src := range []string{"x=[]", "x=[1]", "x=[1,]", "x=[1 # hi\n]"} {
		n := parse(t, src)
		edits, e := (Format{}).Insert([]byte(src), n, n.Get("x"), "-", syntax.Number("2"))
		got := apply(t, src, edits, e)
		items := parse(t, got).Get("x").Items
		if items[len(items)-1].Value != syntax.Number("2") {
			t.Fatal(got)
		}
	}
}
func TestArrayTables(t *testing.T) {
	src := "[[products]] # one\nname='Hammer'\n[[products]] # two\nname='Nail'\n"
	n := parse(t, src)
	edits, e := (Format{}).Insert([]byte(src), n, n.Get("products"), "-", map[string]any{"name": "Saw"})
	got := apply(t, src, edits, e)
	if len(parse(t, got).Get("products").Items) != 3 {
		t.Fatal(got)
	}
	edits, e = (Format{}).Delete([]byte(src), n, n.Get("products").Items[0])
	got = apply(t, src, edits, e)
	if len(parse(t, got).Get("products").Items) != 1 {
		t.Fatal(got)
	}
	if !strings.Contains(got, "# one") {
		t.Fatal(got)
	}
}
func TestNullRejected(t *testing.T) {
	src := "x=1"
	n := parse(t, src)
	if _, e := (Format{}).Replace([]byte(src), n, n.Get("x"), nil); e == nil {
		t.Fatal("null accepted")
	}
}

func TestInlineSyntheticReplace(t *testing.T) {
	for _, src := range []string{"x={a.b=1,a.c=2,z=3}", "x={a.b=1,z=3,a.c=2}", "x={z=3,a.b.c=1,a.b.d=2}"} {
		n := parse(t, src)
		target := n.Get("x").Get("a")
		edits, err := (Format{}).Replace([]byte(src), n, target, "hello")
		out := apply(t, src, edits, err)
		got := parse(t, out).Get("x")
		if got.Get("a").Value != "hello" || got.Get("z").Value != syntax.Number("3") {
			t.Fatal(out)
		}
	}
}
func TestReplaceArrayTableItem(t *testing.T) {
	src := "[[products]] # first\nname='Hammer' # name\n[products.specs]\nweight=1\n[[products]] # second\nname='Nail'\n"
	n := parse(t, src)
	edits, err := (Format{}).Replace([]byte(src), n, n.Get("products").Items[0], map[string]any{"name": "Saw"})
	out := apply(t, src, edits, err)
	items := parse(t, out).Get("products").Items
	if len(items) != 2 || items[0].Get("name").Value != "Saw" || items[0].Get("specs") != nil || items[1].Get("name").Value != "Nail" {
		t.Fatal(out)
	}
	if !strings.Contains(out, "# first") || !strings.Contains(out, "# name") {
		t.Fatal(out)
	}
}
func TestDeleteLastArrayTableItem(t *testing.T) {
	src := "[[products]] # first\nname='Hammer' # name\n"
	n := parse(t, src)
	edits, err := (Format{}).Delete([]byte(src), n, n.Get("products").Items[0])
	out := apply(t, src, edits, err)
	got := parse(t, out).Get("products")
	if got == nil || got.Kind != syntax.Array || len(got.Items) != 0 {
		t.Fatal(out)
	}
}
func TestAppendNestedArrayTableToEarlierItem(t *testing.T) {
	src := "[[products]]\nname='Hammer'\n[[products.reviews]]\nscore=1\n[[products]]\nname='Nail'\n"
	n := parse(t, src)
	edits, err := (Format{}).Insert([]byte(src), n, n.Get("products").Items[0].Get("reviews"), "-", map[string]any{"score": syntax.Number("5")})
	out := apply(t, src, edits, err)
	items := parse(t, out).Get("products").Items
	if len(items[0].Get("reviews").Items) != 2 || items[1].Get("reviews") != nil {
		t.Fatal(out)
	}
}
func TestLimitsAndUnsafeEdits(t *testing.T) {
	for _, src := range []string{"x=" + strings.Repeat("[", 257) + "0" + strings.Repeat("]", 257), strings.Repeat("x.", 256) + "x=1"} {
		if _, err := (Format{}).Parse([]byte(src)); err == nil {
			t.Fatal("nesting limit missing")
		}
	}
	for _, src := range []string{"[a.b]\nx=1\n[a]\nb.y=2", "x=1979-05-27T07:32:00+24:00", "x=1979-05-27T07:32:00+01:60", "x={a={b=1},a.c=2}"} {
		if _, err := (Format{}).Parse([]byte(src)); err == nil {
			t.Fatalf("accepted %q", src)
		}
	}
	n := parse(t, "x=[1]")
	if _, err := (Format{}).Insert([]byte("x=[1]"), n, n.Get("x"), "0", 2); err == nil {
		t.Fatal("array insertion accepted wrong key")
	}
	if _, err := (Format{}).Replace(nil, nil, nil, 1); err == nil {
		t.Fatal("nil target")
	}
}
func TestEscapedControlRendering(t *testing.T) {
	src := "x='old'"
	n := parse(t, src)
	edits, err := (Format{}).Replace([]byte(src), n, n.Get("x"), "a\x7fb")
	out := apply(t, src, edits, err)
	if out != `x="a\u007fb"` {
		t.Fatal(out)
	}
}
func FuzzParse(f *testing.F) {
	for _, s := range []string{"x=1", "[[a]]\nx={foo.bar = [1,2]}", "x='''a'''", "x=1979-05-27T07:32:00Z", "x=[ # a\n1,]"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		n, err := (Format{}).Parse(b)
		if err == nil {
			syntax.Link(n)
			if n.Kind != syntax.Object {
				t.Fatal("non-object root")
			}
		}
	})
}

func TestDeletingLastDottedMemberRetainsEmptyParent(t *testing.T) {
	for _, src := range []string{"a.b.c=1 # stay\n", "x={a.b=1}", "[a.b]\nx=1\n"} {
		n := parse(t, src)
		var target, parent *syntax.Node
		switch {
		case strings.HasPrefix(src, "a.b.c"):
			parent = n.Get("a").Get("b")
			target = parent.Get("c")
		case strings.HasPrefix(src, "x="):
			parent = n.Get("x").Get("a")
			target = parent.Get("b")
		default:
			parent = n.Get("a")
			target = parent.Get("b")
		}
		edits, err := (Format{}).Delete([]byte(src), n, target)
		out := apply(t, src, edits, err)
		after := parse(t, out)
		var got *syntax.Node
		switch {
		case strings.HasPrefix(src, "a.b.c"):
			got = after.Get("a").Get("b")
		case strings.HasPrefix(src, "x="):
			got = after.Get("x").Get("a")
		default:
			got = after.Get("a")
		}
		if got == nil || got.Kind != syntax.Object || len(got.Entries) != 0 {
			t.Fatal(out)
		}
	}
}

func TestDottedKeyDefinesExistingImplicitSupertable(t *testing.T) {
	// Cross-checked with Python's independent TOML 1.0 parser (tomllib).
	for _, src := range []string{"[a.b.c]\n[a]\nb.d=2\n[a.b]", "[a.b.c]\n[a]\nb.d.x=2\n[a.b]"} {
		if _, err := (Format{}).Parse([]byte(src)); err == nil {
			t.Fatalf("accepted table redefinition: %s", src)
		}
	}
	n := parse(t, "[a.b.c]\n[a]\nb.d=2")
	if n.Get("a").Get("b").Get("d").Value != syntax.Number("2") {
		t.Fatal("implicit parent extension")
	}
}
