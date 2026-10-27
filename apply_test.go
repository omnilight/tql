package tql_test

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/edit"
)

func TestApplySourcePreservation(t *testing.T) {
	for _, tc := range []struct{ format, source, want string }{
		{"json", "{\r\n  \"port\" : 80, \"text\": \"\\u0061\", \"n\": 1.00\r\n}\n", "{\r\n  \"port\" : 81, \"text\": \"\\u0061\", \"n\": 1.00\r\n}\n"},
		{"toml", "# keep\r\nport   = 80 # public\r\ntext = 'a'\r\nn = 1.00\r\n", "# keep\r\nport   = 81 # public\r\ntext = 'a'\r\nn = 1.00\r\n"},
		{"yaml", "# keep\r\nport: 80 # public\r\ntext: 'a'\r\nn: 1.00\r\n", "# keep\r\nport: 81 # public\r\ntext: 'a'\r\nn: 1.00\r\n"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			doc := document(t, tc.source, tc.format)
			patch := edit.Patch{edit.Set(edit.Key("port"), int32(81)), edit.Set(edit.Key("text"), "a"), edit.Set(edit.Key("n"), 1)}
			result, err := doc.Apply(patch...)
			if err != nil || result == nil {
				t.Fatalf("Apply: %v", err)
			}
			if string(result.Bytes) != tc.want || !result.Changed || len(result.Values) != 0 {
				t.Fatalf("result = %+v, bytes = %q; want %q", result, result.Bytes, tc.want)
			}
			result.Bytes[0] ^= 1
			if string(doc.Bytes()) != tc.want {
				t.Fatal("result shares source storage with document")
			}
			result, err = doc.Apply(patch...)
			if err != nil || result.Changed || string(result.Bytes) != tc.want {
				t.Fatalf("repeat Apply = %+v, %v", result, err)
			}
		})
	}
}

func TestApplyOrderedCommandsAndCreation(t *testing.T) {
	doc := document(t, `{"a":[1,2,3],"empty":null}`, "json")
	_, err := doc.Apply(
		edit.Del(edit.Key("a").Index(0)),
		edit.Set(edit.Key("a").Index(1), 9),
		edit.Set(edit.Key("a").Index(2), 10),
		edit.Set(edit.Key("empty").Index(0).Key("value"), true),
		edit.Set(edit.Key("new", "nested"), "ok"),
		edit.Del(edit.Key("absent", "child")),
		edit.Del(edit.Key("a").Index(100)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertQuery(t, doc, "a", []any{tql.Number("2"), tql.Number("9"), tql.Number("10")})
	assertQuery(t, doc, "empty[0].value", true)
	assertQuery(t, doc, "new.nested", "ok")
	_, err = doc.Apply(edit.Set(edit.Root(), map[string]any{"x": 1}), edit.Set(edit.Key("x"), 2))
	if err != nil {
		t.Fatal(err)
	}
	assertQuery(t, doc, "x", tql.Number("2"))
}

func TestApplyValuesAndPathsAreSnapshots(t *testing.T) {
	base := edit.Key("a.b")
	left, right := base.Key("left"), base.Key("right")
	if base.String() != `.["a.b"]` || left.String() != `.["a.b"]["left"]` || right.String() != `.["a.b"]["right"]` {
		t.Fatal("extending paths changed a shared path")
	}
	value := map[string]any{"items": []any{int8(1), uint64(math.MaxUint64), float32(0.1)}}
	op := edit.Set(left, value)
	value["items"].([]any)[0] = 5
	op.Value().(map[string]any)["items"].([]any)[0] = 7
	doc := document(t, `{}`, "json")
	key := "x.\"; del(a); #\x01"
	_, err := doc.Apply(op, edit.Set(edit.Key(key), key))
	if err != nil {
		t.Fatal(err)
	}
	if op.Kind() != edit.SetKind || op.Path().String() != left.String() {
		t.Fatal("operation inspection failed")
	}
	want := map[string]any{
		"a.b": map[string]any{"left": map[string]any{"items": []any{tql.Number("1"), tql.Number("18446744073709551615"), tql.Number("0.1")}}},
		key:   key,
	}
	if !reflect.DeepEqual(doc.Value(), want) {
		t.Fatalf("got %#v; want %#v", doc.Value(), want)
	}
	other := document(t, `{}`, "json")
	if _, err := other.Apply(op); err != nil {
		t.Fatal(err)
	}
	if op.Value().(map[string]any)["items"].([]any)[0] != tql.Number("1") {
		t.Fatal("applying reused command changed its value")
	}
}

func TestApplyRejectsInvalidCommandsAtomically(t *testing.T) {
	cyclicMap := map[string]any{}
	cyclicMap["self"] = cyclicMap
	cyclicSlice := make([]any, 1)
	cyclicSlice[0] = cyclicSlice
	var deep any = true
	for i := 0; i < 258; i++ {
		deep = []any{deep}
	}
	invalid := []edit.Operation{
		{}, edit.Del(edit.Root()), edit.Set(edit.Key("x").Index(-1), 0),
		edit.Set(edit.Key(string([]byte{0xff})), true),
		edit.Set(edit.Key("x"), string([]byte{0xff})),
		edit.Set(edit.Key("x"), map[string]any{string([]byte{0xff}): true}),
		edit.Set(edit.Key("x"), tql.Number("1/2")),
		edit.Set(edit.Key("x"), tql.Temporal("2026-02-30")),
		edit.Set(edit.Key("x"), tql.Temporal("2026-01-01T00:00:00+24:00")),
		edit.Set(edit.Key("x"), math.NaN()), edit.Set(edit.Key("x"), float32(math.Inf(1))),
		edit.Set(edit.Key("x"), map[string]int{"a": 1}), edit.Set(edit.Key("x"), []string{"a"}),
		edit.Set(edit.Key("x"), json.RawMessage(`{}`)), edit.Set(edit.Key("x"), json.Number("1")),
		edit.Set(edit.Key("x"), cyclicMap), edit.Set(edit.Key("x"), cyclicSlice), edit.Set(edit.Key("x"), deep),
		edit.Set(edit.Key("x").Index(3), 1), edit.Set(edit.Key("port", "child"), true),
		edit.Del(edit.Key("port", "child")), edit.Set(edit.Key("port").Index(0), 1),
	}
	for i, op := range invalid {
		doc := document(t, `{"port":80}`, "json")
		before := doc.Bytes()
		r, err := doc.Apply(edit.Set(edit.Key("port"), 81), op)
		if err == nil || r != nil || !bytes.Equal(doc.Bytes(), before) || !strings.Contains(err.Error(), "operation 1") {
			t.Fatalf("case %d: result = %+v, error = %v, bytes = %q", i, r, err, doc.Bytes())
		}
	}
}

func TestApplyTOMLTypedValuesAndFormatRollback(t *testing.T) {
	doc := document(t, "# keep\nport = 80\n", "toml")
	_, err := doc.Apply(
		edit.Set(edit.Key("date"), tql.Temporal("2026-10-26")),
		edit.Set(edit.Key("special"), tql.Number("+nan")),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertQuery(t, doc, "date", tql.Temporal("2026-10-26"))
	assertQuery(t, doc, "special", tql.Number("+nan"))
	before := doc.Bytes()
	if r, err := doc.Apply(edit.Set(edit.Key("port"), 81), edit.Set(edit.Key("bad"), nil)); err == nil || r != nil || !bytes.Equal(before, doc.Bytes()) {
		t.Fatalf("format failure did not roll back: %+v, %v", r, err)
	}
	for _, format := range []string{"json", "yaml"} {
		for _, v := range []any{tql.Temporal("2026-10-26"), tql.Number("inf"), map[string]any{"nested": tql.Number("-nan")}} {
			d := document(t, "{}", format)
			if r, err := d.Apply(edit.Set(edit.Key("bad"), v)); err == nil || r != nil || string(d.Bytes()) != "{}" {
				t.Fatalf("%s accepted lossy value %v: %+v, %v", format, v, r, err)
			}
		}
	}
}

func TestApplyEmptyAndNilDocuments(t *testing.T) {
	for _, d := range []*tql.Document{nil, {}} {
		if r, err := d.Apply(); err == nil || r != nil {
			t.Fatalf("uninitialized Apply = %+v, %v", r, err)
		}
	}
	doc := document(t, "{}", "json")
	r, err := doc.Apply()
	if err != nil || r.Changed || string(r.Bytes) != "{}" || len(r.Values) != 0 {
		t.Fatalf("empty Apply = %+v, %v", r, err)
	}
}

func TestApplySharedAcyclicValuesAndNilContainers(t *testing.T) {
	shared := map[string]any{"ok": true}
	items := make([]any, 2)
	items[1] = items[:1] // Same backing array, but a different acyclic slice.
	doc := document(t, "{}", "json")
	_, err := doc.Apply(edit.Set(edit.Root(), map[string]any{
		"shared": []any{shared, shared}, "items": items,
		"object": map[string]any(nil), "array": []any(nil), "null": nil,
	}))
	if err != nil {
		t.Fatal(err)
	}
	assertQuery(t, doc, "object", map[string]any{})
	assertQuery(t, doc, "array", []any{})
	assertQuery(t, doc, "null", nil)
	assertQuery(t, doc, "items", []any{nil, []any{nil}})
}
