package query

import (
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestTreeEqualityMatchesDetachedValues(t *testing.T) {
	values := []any{
		nil, true, "1", syntax.Number("1"), syntax.Number("1.0"),
		syntax.Number("-0"), syntax.Number("0.0"), syntax.Number("nan"), syntax.Number("+nan"),
		syntax.Temporal("2026-10-29"), []any{}, map[string]any{},
		map[string]any{"absent": nil}, map[string]any{"other": nil},
		[]any{syntax.Number("1"), map[string]any{"x": []any{true, nil}}},
		[]any{syntax.Number("1.0"), map[string]any{"x": []any{true, nil}}},
		map[string]any{"a": syntax.Number("2"), "b": false},
	}
	for _, a := range values {
		for _, b := range values {
			want := Equal(a, b)
			if got := EqualNodes(Node(a), Node(b)); got != want {
				t.Fatalf("nodes %v / %v: %v want %v", a, b, got, want)
			}
			if got := EqualNodeValue(Node(a), b); got != want {
				t.Fatalf("value %v / %v: %v want %v", a, b, got, want)
			}
		}
	}
	// Object order has no semantic significance; parent metadata is ignored.
	a := Node(map[string]any{"a": true, "b": false})
	b := Node(a.Interface())
	b.Entries[0], b.Entries[1] = b.Entries[1], b.Entries[0]
	if !EqualNodes(a, b) {
		t.Fatal("object ordering affected equality")
	}
}
