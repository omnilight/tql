package tql_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
	jsonformat "github.com/vasileknik76/tql/formats/json"
	"github.com/vasileknik76/tql/syntax"
)

type parseCountingFormat struct {
	tql.Format
	parses int
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
