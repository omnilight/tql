package tql_test

import (
	"testing"

	"github.com/vasileknik76/tql"
	jsonformat "github.com/vasileknik76/tql/formats/json"
)

func TestRegistryZeroValueAndNameNormalization(t *testing.T) {
	var registry tql.Registry
	if err := registry.Register(" Custom ", jsonformat.Format{}); err != nil {
		t.Fatal(err)
	}
	doc, err := registry.Parse([]byte(`{"n":1}`), " CUSTOM ")
	if err != nil {
		t.Fatal(err)
	}
	values, err := doc.Query(`n`)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0] != tql.Number("1") {
		t.Fatalf("values=%v", values)
	}
	if err := registry.Register("custom", jsonformat.Format{}); err == nil {
		t.Fatal("normalized duplicate name accepted")
	}
}
