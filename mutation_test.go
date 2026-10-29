package tql_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
	jsonformat "github.com/vasileknik76/tql/formats/json"
	"github.com/vasileknik76/tql/formats/toml"
	"github.com/vasileknik76/tql/syntax"
)

type parseCountingFormat struct {
	tql.Format
	parses int
}

func TestIndependentDeletionsParseOnce(t *testing.T) {
	for _, tc := range []struct {
		format               tql.Format
		source, script, want string
	}{
		{toml.Format{}, "a=1\nb=2\nc=3\nkeep=4\n", `del(a,b,c)`, "keep=4\n"},
		{toml.Format{}, "[[items]]\nx=1\n\n[[items]]\nx=2\n\n[[items]]\nx=3\n", `del(items[0:2])`, "[[items]]\nx=3\n"},
		{jsonformat.Format{}, `{"a":1,"b":2,"keep":3}`, `del(a,b)`, `{"keep":3}`},
	} {
		format := &parseCountingFormat{Format: tc.format}
		doc, err := tql.ParseWith([]byte(tc.source), format)
		if err != nil {
			t.Fatal(err)
		}
		before := format.parses
		result, err := doc.Exec(tc.script)
		if err != nil {
			t.Fatal(err)
		}
		if format.parses-before != 1 {
			t.Fatalf("%s: got %d parses", tc.script, format.parses-before)
		}
		if string(result.Bytes) != tc.want {
			t.Fatalf("got %q want %q", result.Bytes, tc.want)
		}
	}
}

func TestDeleteBatchReferencesAndFallback(t *testing.T) {
	for _, tc := range []struct{ source, script, want string }{
		{"[[items]]\nx=1\n\n[[items]]\nx=2\n\n[[items]]\nx=3\n", `for $item in items[] { if $item.x == 1 { del($.items[0:2]); } else { $item.x = 4; } }`, "[[items]]\nx=4\n"},
		// Removing every array-table item requires an explicit empty array.
		{"[[items]]\nx=1\n\n[[items]]\nx=2\n", `del(items[])`, "items = []\n\n"},
		// Two deletions can own the same comma; use sequential edits then.
		{"items=[1,2,3]\n", `del(items[1:3])`, "items=[1]\n"},
	} {
		doc := document(t, tc.source, "toml")
		result, err := doc.Exec(tc.script)
		if err != nil {
			t.Fatal(err)
		}
		if string(result.Bytes) != tc.want {
			t.Fatalf("got %q want %q", result.Bytes, tc.want)
		}
	}
	// A later failure must roll back a successful deletion batch and its refs.
	doc := document(t, "a=1\nb=2\nkeep=3\n", "toml")
	if _, err := doc.Exec(`del(a,b); keep=null`); err == nil {
		t.Fatal("expected TOML failure")
	}
	if string(doc.Bytes()) != "a=1\nb=2\nkeep=3\n" {
		t.Fatal("failed batch committed")
	}
}

func (f *parseCountingFormat) Parse(source []byte) (*syntax.Node, error) {
	f.parses++
	return f.Format.Parse(source)
}

func TestIndependentReplacementsParseOnce(t *testing.T) {
	format := &parseCountingFormat{Format: jsonformat.Format{}}
	source := wideSource(256)
	doc, err := tql.ParseWith(source, format)
	if err != nil {
		t.Fatal(err)
	}
	before := format.parses
	result, err := doc.Exec(`.[] += 1`)
	if err != nil {
		t.Fatal(err)
	}
	if got := format.parses - before; got != 1 {
		t.Fatalf("independent replacements reparsed %d times, want 1", got)
	}
	var want strings.Builder
	want.WriteByte('{')
	for i := range 256 {
		if i > 0 {
			want.WriteByte(',')
		}
		fmt.Fprintf(&want, `"key%d":%d`, i, i+1)
	}
	want.WriteByte('}')
	if string(result.Bytes) != want.String() {
		t.Fatalf("unexpected rewritten source: %s", result.Bytes)
	}
	if string(source) != string(wideSource(256)) {
		t.Fatal("execution changed the caller's input")
	}
}
