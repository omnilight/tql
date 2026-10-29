package edit_test

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/edit"
	"github.com/vasileknik76/tql/internal/query"
)

func parse(t *testing.T, text, format string) *tql.Document {
	t.Helper()
	doc, err := tql.Parse([]byte(text), format)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestDiffDeterministicAndDetached(t *testing.T) {
	a := parse(t, `{"z":1,"items":[{"old":true,"n":1},2,3,4],"null":null,"same":1.00}`, "json")
	b := parse(t, `{"items":[{"n":2},9],"new":{"a.b":true},"same":1}`, "json")
	aBefore, bBefore := a.Bytes(), b.Bytes()
	patch, err := edit.Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	text, err := patch.TQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "del(.[\"null\"]);\ndel(.[\"z\"]);\ndel(.[\"items\"][0][\"old\"]);\n.[\"items\"][0][\"n\"] = 2;\n.[\"items\"][1] = 9;\ndel(.[\"items\"][3]);\ndel(.[\"items\"][2]);\n.[\"new\"] = {\"a.b\":true};\n"
	if text != want {
		t.Fatalf("TQL = %s; want %s", text, want)
	}
	if !bytes.Equal(a.Bytes(), aBefore) || !bytes.Equal(b.Bytes(), bBefore) {
		t.Fatal("planning changed a document")
	}
	for range 10 {
		again, err := edit.Diff(a, b)
		if err != nil {
			t.Fatal(err)
		}
		script, err := again.TQL()
		if err != nil || script != text {
			t.Fatalf("nondeterministic diff: %q, %v", script, err)
		}
	}
	wantValue := b.Value()
	if _, err := b.Exec(`. = null`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Apply(patch...); err != nil {
		t.Fatal(err)
	}
	if !query.Equal(a.Value(), wantValue) || !strings.Contains(string(a.Bytes()), `"same":1.00`) {
		t.Fatalf("Apply diff = %s", a.Bytes())
	}
}

func TestDiffAndExportPreserveSource(t *testing.T) {
	for _, tc := range []struct{ format, source, target, want string }{
		{"json", "{\n  \"a.b\" : {\"items\":[1, 2]},\n  \"same\":\"\\u0061\"\n}\n", `{"a.b":{"items":[3,2,4]},"same":"a"}`, "{\n  \"a.b\" : {\"items\":[3, 2, 4]},\n  \"same\":\"\\u0061\"\n}\n"},
		{"toml", "# keep\r\nport = 80 # public\r\nsame = 'a'\r\n", "port = 81\nsame = 'a'\n", "# keep\r\nport = 81 # public\r\nsame = 'a'\r\n"},
		{"yaml", "# keep\r\nport: 80 # public\r\nsame: 'a'\r\n", "port: 81\nsame: a\n", "# keep\r\nport: 81 # public\r\nsame: 'a'\r\n"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			a, b := parse(t, tc.source, tc.format), parse(t, tc.target, tc.format)
			patch, err := edit.Diff(a, b)
			if err != nil {
				t.Fatal(err)
			}
			typed, err := a.Apply(patch...)
			if err != nil {
				t.Fatal(err)
			}
			text, err := patch.TQL()
			if err != nil {
				t.Fatal(err)
			}
			textual, err := parse(t, tc.source, tc.format).Exec(text)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(typed.Bytes, textual.Bytes) || string(typed.Bytes) != tc.want {
				t.Fatalf("typed = %q; textual = %q; want %q", typed.Bytes, textual.Bytes, tc.want)
			}
			noop, err := edit.Diff(a, b)
			if err != nil || len(noop) != 0 {
				t.Fatalf("repeat diff = %v, %v", noop, err)
			}
		})
	}
}

func TestDiffCrossFormatAndTOMLValues(t *testing.T) {
	for _, tc := range []struct{ source, target, fromFormat, toFormat string }{
		{"port: 80\n", `{"port":81,"nested":[null,true]}`, "yaml", "json"},
		{"date = 2026-01-01\nspecial = inf\n", "date = 2026-10-26\nspecial = -nan\n", "toml", "toml"},
		{`[]`, `[{"a":1},2,3]`, "json", "json"},
		{`{"a":null}`, `null`, "json", "json"},
	} {
		a, b := parse(t, tc.source, tc.fromFormat), parse(t, tc.target, tc.toFormat)
		patch, err := edit.Diff(a, b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Apply(patch...); err != nil {
			t.Fatal(err)
		}
		if !query.Equal(a.Value(), b.Value()) {
			t.Fatalf("diff did not reach target: %s", a.Bytes())
		}
	}
	a, b := parse(t, "port = 80\n", "toml"), parse(t, `{"port":null}`, "json")
	patch, err := edit.Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := a.Apply(patch...); err == nil || r != nil || string(a.Bytes()) != "port = 80\n" {
		t.Fatalf("unrepresentable diff = %+v, %v", r, err)
	}
}

func TestMergePolicies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options []edit.MergeOption
		want    string
	}{
		{"default", nil, `{"custom":true,"items":[2,3],"options":{"old":true,"port":81},"null":null}`},
		{"append", []edit.MergeOption{edit.WithArrays(edit.AppendArrays)}, `{"custom":true,"items":[1,2,2,3],"options":{"old":true,"port":81},"null":null}`},
		{"union", []edit.MergeOption{edit.WithArrays(edit.UnionArrays)}, `{"custom":true,"items":[1,2,3],"options":{"old":true,"port":81},"null":null}`},
		{"replace", []edit.MergeOption{edit.WithReplace(edit.Key("options"))}, `{"custom":true,"items":[2,3],"options":{"port":81},"null":null}`},
		{"root", []edit.MergeOption{edit.WithReplace(edit.Root())}, `{"items":[2,3],"options":{"port":81},"null":null}`},
		{"last_mode", []edit.MergeOption{edit.WithArrays(edit.AppendArrays), edit.WithArrays(edit.UnionArrays)}, `{"custom":true,"items":[1,2,3],"options":{"old":true,"port":81},"null":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := parse(t, `{"custom":true,"items":[1,2],"options":{"old":true,"port":80},"null":1}`, "json")
			b := parse(t, `{"items":[2,3],"options":{"port":81},"null":null}`, "json")
			aBefore, bBefore := a.Bytes(), b.Bytes()
			patch, err := edit.Merge(a, b, tc.options...)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a.Bytes(), aBefore) || !bytes.Equal(b.Bytes(), bBefore) {
				t.Fatal("Merge changed its inputs")
			}
			if _, err := a.Apply(patch...); err != nil {
				t.Fatal(err)
			}
			if !query.Equal(a.Value(), parse(t, tc.want, "json").Value()) {
				t.Fatalf("merged = %s; want %s", a.Bytes(), tc.want)
			}
			if tc.name != "append" {
				again, err := edit.Merge(a, b, tc.options...)
				if err != nil || len(again) != 0 {
					t.Fatalf("merge not idempotent: %v, %v", again, err)
				}
			}
		})
	}
}

func TestMergeUnionUsesExactRecursiveEquality(t *testing.T) {
	a := parse(t, `{"items":[1.0,1.0,{"n":1.0},9007199254740992,1e999999]}`, "json")
	b := parse(t, `{"items":[1,{"n":1e0},9007199254740993,10e999998,3,3]}`, "json")
	patch, err := edit.Merge(a, b, edit.WithArrays(edit.UnionArrays))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Apply(patch...); err != nil {
		t.Fatal(err)
	}
	want := `{"items":[1.0,1.0,{"n":1.0},9007199254740992,1e999999,9007199254740993,3]}`
	if string(a.Bytes()) != want {
		t.Fatalf("union = %s; want %s", a.Bytes(), want)
	}
}

func TestMergeReplaceOptionsAndInvalidInputs(t *testing.T) {
	a, b := parse(t, `{"a.b":{"keep":true,"x":1},"items":[1],"other":true}`, "json"), parse(t, `{"a.b":{"x":2},"items":[2]}`, "json")
	paths := []edit.Path{edit.Key("a.b")}
	option := edit.WithReplace(paths...)
	paths[0] = edit.Key("missing")
	patch, err := edit.Merge(a, b, option, edit.WithReplace(edit.Key("a.b"), edit.Key("a.b", "x")), edit.WithReplace(edit.Key("items")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Apply(patch...); err != nil {
		t.Fatal(err)
	}
	if !query.Equal(a.Value(), parse(t, `{"a.b":{"x":2},"items":[2],"other":true}`, "json").Value()) {
		t.Fatalf("replacement = %s", a.Bytes())
	}
	for _, option := range []edit.MergeOption{
		{}, edit.WithArrays(edit.ArrayMode(99)), edit.WithReplace(edit.Key("missing")),
		edit.WithReplace(edit.Key("items").Index(0)), edit.WithReplace(edit.Key("a.b", "x", "bad")),
		edit.WithReplace(edit.Key("items").Index(-1)), edit.WithReplace(edit.Key(string([]byte{0xff}))),
	} {
		if p, err := edit.Merge(b, b, option); err == nil || p != nil {
			t.Fatalf("invalid option accepted: %v, %v", p, err)
		}
	}
	for _, invalid := range []*tql.Document{nil, {}} {
		for _, pair := range [][2]*tql.Document{{invalid, a}, {a, invalid}} {
			if p, err := edit.Diff(pair[0], pair[1]); err == nil || p != nil {
				t.Fatalf("invalid Diff inputs: %v, %v", p, err)
			}
			if p, err := edit.Merge(pair[0], pair[1]); err == nil || p != nil {
				t.Fatalf("invalid Merge inputs: %v, %v", p, err)
			}
		}
	}
}

func TestTQLExportCompositionAndErrors(t *testing.T) {
	key := "a.b\"; del(keep); #\x01"
	patch := edit.Patch{
		edit.Set(edit.Key(key), map[string]any{"text": key}),
		edit.Del(edit.Key("items").Index(0)),
		edit.Set(edit.Key("items").Index(0), 9),
		edit.Del(edit.Key("missing")),
	}
	text, err := patch.TQL()
	if err != nil {
		t.Fatal(err)
	}
	const source = `{"keep":true,"items":[1,2]}`
	a, b := parse(t, source, "json"), parse(t, source, "json")
	if _, err := a.Apply(patch...); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Exec(text); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatalf("typed = %s; exported = %s", a.Bytes(), b.Bytes())
	}
	noop, err := (edit.Patch{}).TQL()
	if err != nil {
		t.Fatal(err)
	}
	before := b.Bytes()
	if r, err := b.Exec(noop + noop); err != nil || r.Changed || !bytes.Equal(before, b.Bytes()) || len(r.Values) != 0 {
		t.Fatalf("empty export = %+v, %v", r, err)
	}
	for _, invalid := range []edit.Operation{
		{}, edit.Del(edit.Root()), edit.Set(edit.Key("x").Index(-1), true),
		edit.Set(edit.Key("x"), tql.Temporal("2026-10-26")),
		edit.Set(edit.Key("x"), []any{map[string]any{"n": tql.Number("+inf")}}),
	} {
		if source, err := (edit.Patch{edit.Set(edit.Key("ok"), true), invalid}).TQL(); err == nil || source != "" || !strings.Contains(err.Error(), "operation 1") {
			t.Fatalf("invalid export = %q, %v", source, err)
		}
	}
	failing := edit.Patch{edit.Set(edit.Key("keep"), false), edit.Set(edit.Key("items").Index(10), true)}
	text, err = failing.TQL()
	if err != nil {
		t.Fatal(err)
	}
	for _, typed := range []bool{false, true} {
		d := parse(t, source, "json")
		if typed {
			_, err = d.Apply(failing...)
		} else {
			_, err = d.Exec(text)
		}
		if err == nil || string(d.Bytes()) != source {
			t.Fatalf("failure did not roll back (typed %v): %v", typed, err)
		}
	}
}

func ExampleDiff() {
	a, err := tql.Parse([]byte("# listener\nport = 80 # keep\n"), "toml")
	if err != nil {
		panic(err)
	}
	b, err := tql.Parse([]byte("port = 81\n"), "toml")
	if err != nil {
		panic(err)
	}
	patch, err := edit.Diff(a, b)
	if err != nil {
		panic(err)
	}
	text, err := patch.TQL()
	if err != nil {
		panic(err)
	}
	fmt.Print(text)
	result, err := a.Apply(patch...)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(result.Bytes))
	// Output:
	// .["port"] = 81;
	// # listener
	// port = 81 # keep
}

func FuzzDiff(f *testing.F) {
	f.Add([]byte(`{"items":[1,2,3],"old":true}`), []byte(`{"items":[4],"new":null}`))
	f.Add([]byte(`null`), []byte(`[true,{"a.b":1}]`))
	f.Add([]byte(`{"n":1.00}`), []byte(`{"n":1}`))
	f.Fuzz(func(t *testing.T, from, to []byte) {
		a, err := tql.Parse(from, "json")
		if err != nil {
			return
		}
		b, err := tql.Parse(to, "json")
		if err != nil {
			return
		}
		before := a.Bytes()
		patch, err := edit.Diff(a, b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Apply(patch...); err != nil {
			t.Fatal(err)
		}
		if !query.Equal(a.Value(), b.Value()) {
			t.Fatalf("diff of %s to %s reached %s", from, to, a.Bytes())
		}
		if !reflect.DeepEqual(before, from) {
			t.Fatal("planning modified input")
		}
	})
}
