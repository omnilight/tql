package json

import (
	stdjson "encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func parse(t *testing.T, source string) *syntax.Node {
	t.Helper()
	n, err := (Format{}).Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSpecValues(t *testing.T) {
	cases := []struct {
		source string
		value  any
	}{
		{`null`, nil}, {`true`, true}, {`false`, false},
		{`""`, ""}, {`"é/水😀"`, "é/水😀"},
		{`"\"\\\/\b\f\n\r\t\u0000"`, "\"\\/\b\f\n\r\t\x00"},
		{`"\u0061\u20AC\uD834\uDD1E"`, "a€𝄞"},
		{`-0`, syntax.Number("-0")}, {`1.2300E+999999`, syntax.Number("1.2300E+999999")},
		{`123456789012345678901234567890`, syntax.Number("123456789012345678901234567890")},
		{`[null, false, "x", {"a":2}]`, []any{nil, false, "x", map[string]any{"a": syntax.Number("2")}}},
		{" \t\r\n{\"a\":[],\"b\":{}}\r\n", map[string]any{"a": []any{}, "b": map[string]any{}}},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			n := parse(t, tc.source)
			if !reflect.DeepEqual(n.Interface(), tc.value) {
				t.Fatalf("got %#v, want %#v", n.Interface(), tc.value)
			}
		})
	}
}

func TestRejectInvalid(t *testing.T) {
	cases := []string{
		"", " ", "\ufeff{}", "\vnull", "null\f", "nul", "True", "undefined", "null true", "1x",
		"+1", ".1", "01", "-01", "-", "1.", "1e", "1e+", "--1", "NaN", "Infinity",
		"[", "[1,]", "[,1]", "[1 2]", "{", "{a:1}", "{\"a\" 1}", "{\"a\":1,}",
		`"\x20"`, `"\u000g"`, `"\u123"`, `"\uD800"`, `"\uDC00"`, `"\uD800\u0041"`,
		"\"a\nb\"", "\"\x00\"", "\"unterminated", "\"\\", "\"\xff\"",
		`{"a":1,"a":2}`, `{"a":1,"\u0061":2}`, `/* comment */{}`,
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if _, err := (Format{}).Parse([]byte(s)); err == nil {
				t.Fatalf("accepted %q", s)
			}
		})
	}
}

func TestSourceSpans(t *testing.T) {
	source := " \r\n{ \"é\" : [ \"😀\", -0.50e+2 ] , \"a\":null }\n"
	n := parse(t, source)
	if source[n.Start:n.End] != `{ "é" : [ "😀", -0.50e+2 ] , "a":null }` {
		t.Fatal("bad root span")
	}
	e := n.Entries[0]
	if source[e.KeyStart:e.KeyEnd] != `"é"` || source[e.Start:e.End] != `"é" : [ "😀", -0.50e+2 ]` {
		t.Fatalf("bad entry spans: %#v", e)
	}
	if e.Value.Parent != n || e.Value.Items[0].Parent != e.Value {
		t.Fatal("missing parent link")
	}
	if source[e.Value.Items[0].Start:e.Value.Items[0].End] != `"😀"` {
		t.Fatal("string span must count UTF-8 bytes")
	}
}

func TestSurgicalReplace(t *testing.T) {
	source := "\r\n{\r\n  \"é\" : [1, 2.00e0],\r\n  \"unchanged\" : \"\\u0061\"\r\n}\t"
	n := parse(t, source)
	target := n.Get("é").Items[1]
	edits, err := (Format{}).Replace([]byte(source), n, target, syntax.Number("9007199254740993"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := syntax.Apply([]byte(source), edits)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(source, "2.00e0", "9007199254740993", 1)
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	parse(t, string(got))
}

func TestSurgicalDelete(t *testing.T) {
	cases := []struct {
		source, key, want string
		index             int
	}{
		{`{ "a":1,  "b":2, "c":3 }`, "a", `{   "b":2, "c":3 }`, -1},
		{`{ "a":1,  "b":2, "c":3 }`, "b", `{ "a":1,   "c":3 }`, -1},
		{`{ "a":1,  "b":2, "c":3 }`, "c", `{ "a":1,  "b":2  }`, -1},
		{"{\r\n  \"a\": 1\r\n}\n", "a", "{\r\n  \r\n}\n", -1},
		{`[ 1,  2, 3 ]`, "", `[   2, 3 ]`, 0},
		{`[ 1,  2, 3 ]`, "", `[ 1,   3 ]`, 1},
		{`[ 1,  2, 3 ]`, "", `[ 1,  2  ]`, 2},
		{`[ 1 ]`, "", `[  ]`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.source+tc.key, func(t *testing.T) {
			n := parse(t, tc.source)
			target := n.Get(tc.key)
			if tc.index >= 0 {
				target = n.Items[tc.index]
			}
			edits, err := (Format{}).Delete([]byte(tc.source), n, target)
			if err != nil {
				t.Fatal(err)
			}
			out, err := syntax.Apply([]byte(tc.source), edits)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tc.want {
				t.Fatalf("got %q, want %q", out, tc.want)
			}
			parse(t, string(out))
		})
	}
}

func TestSurgicalInsert(t *testing.T) {
	cases := []struct {
		source, key, want string
		value             any
	}{
		{`{}`, "x", `{"x":1}`, 1},
		{`{ }`, "x", `{ "x":1 }`, 1},
		{`{"a": 2}`, "x", `{"a": 2,"x": 1}`, 1},
		{`{ "a": 2 }`, "x", `{ "a": 2, "x": 1 }`, 1},
		{`{"a":2,  "b":3}`, "x", `{"a":2,  "b":3,  "x":1}`, 1},
		{"{\r\n\t\"a\" : 2\r\n}\n", "x", "{\r\n\t\"a\" : 2,\r\n\t\"x\" : 1\r\n}\n", 1},
		{"{\n}", "x", "{\n  \"x\": 1\n}", 1},
		{"{\n}", `a":b`, "{\n  \"a\\\":b\": 1\n}", 1},
		{`[]`, "-", `[1]`, 1},
		{`[1, 2]`, "2", `[1, 2, 1]`, 1},
		{`[1,  2]`, "-", `[1,  2,  1]`, 1},
		{"[\n  2\n]\t", "-", "[\n  2,\n  1\n]\t", 1},
	}
	for _, tc := range cases {
		t.Run(tc.source+tc.key, func(t *testing.T) {
			n := parse(t, tc.source)
			edits, err := (Format{}).Insert([]byte(tc.source), n, n, tc.key, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			out, err := syntax.Apply([]byte(tc.source), edits)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tc.want {
				t.Fatalf("got %q, want %q", out, tc.want)
			}
			parse(t, string(out))
		})
	}
}

func TestEditErrors(t *testing.T) {
	f := Format{}
	n := parse(t, `{"a":1}`)
	if _, err := f.Insert([]byte(`{"a":1}`), n, n, "a", 2); err == nil {
		t.Fatal("duplicate insertion accepted")
	}
	if _, err := f.Delete([]byte(`{"a":1}`), n, n); err == nil {
		t.Fatal("root deletion accepted")
	}
	a := parse(t, `[1]`)
	if _, err := f.Insert([]byte(`[1]`), a, a, "0", 2); err == nil {
		t.Fatal("non-append insertion accepted")
	}
	for _, value := range []any{syntax.Number("01"), syntax.Number("NaN"), math.Inf(1), math.NaN(), make(chan int)} {
		if _, err := f.Replace([]byte(`{"a":1}`), n, n.Get("a"), value); err == nil {
			t.Fatalf("invalid value %v accepted", value)
		}
	}
}

func TestDepthLimit(t *testing.T) {
	s := strings.Repeat("[", 1026) + "0" + strings.Repeat("]", 1026)
	if _, err := (Format{}).Parse([]byte(s)); err == nil {
		t.Fatal("excessive depth accepted")
	}
}

// Every accepted input must also be accepted by the independent standard-library
// parser. Re-encoding the source tree checks decoding and exact numeric values.
func FuzzParse(f *testing.F) {
	for _, s := range []string{`null`, `{"a":[1,"x",true]}`, `"\uD834\uDD1E"`, `[1.5e+20,-0]`, `{"a":1,"a":2}`, `"\uD800"`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		n, err := (Format{}).Parse(b)
		if err != nil {
			return
		}
		if !stdjson.Valid(b) {
			t.Fatalf("accepted invalid JSON %q", b)
		}
		encoded, err := syntax.JSON(n.Interface())
		if err != nil {
			t.Fatal(err)
		}
		var original, roundtrip any
		d := stdjson.NewDecoder(strings.NewReader(string(b)))
		d.UseNumber()
		if err = d.Decode(&original); err != nil {
			t.Fatal(err)
		}
		d = stdjson.NewDecoder(strings.NewReader(encoded))
		d.UseNumber()
		if err = d.Decode(&roundtrip); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, roundtrip) {
			t.Fatalf("decoded values differ: %#v versus %#v", original, roundtrip)
		}
	})
}
