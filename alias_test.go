package tql_test

import (
	"bytes"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestYAMLAliasMaterialization(t *testing.T) {
	source := "defaults: &base\n  port: 80 # keep\n  enabled: false\ncopy: *base # local\nother: *base\n"
	doc, e := tql.Parse([]byte(source), "yaml")
	if e != nil {
		t.Fatal(e)
	}
	vals, e := doc.Query(`copy.port`)
	if e != nil {
		t.Fatal(e)
	}
	if len(vals) != 1 || vals[0] != tql.Number("80") {
		t.Fatalf("values=%v", vals)
	}
	result, e := doc.Exec(`copy.port = 443; copy.extra = true; del(copy.enabled)`)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.HasPrefix(result.Bytes, []byte("defaults: &base\n  port: 80 # keep\n  enabled: false\n")) {
		t.Fatalf("anchor source changed: %s", result.Bytes)
	}
	if !bytes.HasSuffix(result.Bytes, []byte(" # local\nother: *base\n")) {
		t.Fatalf("alias siblings/comments changed: %s", result.Bytes)
	}
	vals, e = doc.Query(`[defaults.port, copy.port, other.port, copy.extra]`)
	if e != nil {
		t.Fatal(e)
	}
	got := vals[0].([]any)
	if got[0] != tql.Number("80") || got[1] != tql.Number("443") || got[2] != tql.Number("80") || got[3] != true {
		t.Fatalf("values=%v", vals)
	}
}
func TestYAMLAnchorSideEffectsRollBack(t *testing.T) {
	source := []byte("base: &a {n: 1}\ncopy: *a\n")
	doc, e := tql.Parse(source, "yaml")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = doc.Exec(`base.n = 2`); e == nil {
		t.Fatal("anchor edit silently changed an unselected alias")
	}
	if !bytes.Equal(doc.Bytes(), source) {
		t.Fatal("failed anchored edit committed")
	}
}
