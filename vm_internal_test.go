package tql

import (
	"fmt"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func values(t *testing.T, result *Result) []string {
	t.Helper()
	out := make([]string, len(result.Values))
	for i, value := range result.Values {
		s, err := syntax.JSON(value)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = s
	}
	return out
}

func TestVMPathReads(t *testing.T) {
	source := `{"a":[1,2,3],"b":{"x":1,"y":2},"items":[{"name":"api","n":1},{"name":"web","n":2}],"s":"café"}`
	tests := []struct {
		program string
		want    []string
	}{
		{`a`, []string{"[1,2,3]"}},
		{`a[0]`, []string{"1"}},
		{`a[-1]`, []string{"3"}},
		{`a[*]`, []string{"1", "2", "3"}},
		{`a[0:2]`, []string{"1", "2"}},
		{`a[] | select(. > 1)`, []string{"2", "3"}},
		{`b.x`, []string{"1"}},
		{`b.*`, []string{"1", "2"}},
		{`items[name == "api"].n`, []string{"1"}},
		{`items[].n`, []string{"1", "2"}},
		{`{x: 1, y: a}`, []string{`{"x":1,"y":[1,2,3]}`}},
		{`[1, 2, a[]]`, []string{"[1,2,1,2,3]"}},
		{`walk(a) | select(type == "number")`, []string{"1", "2", "3"}},
		{`path(a[1])`, []string{`["a",1]`}},
		{`key(a[1])`, []string{"1"}},
		{`s`, []string{`"café"`}},
	}
	for _, test := range tests {
		t.Run(test.program, func(t *testing.T) {
			doc, err := Parse([]byte(source), "json")
			if err != nil {
				t.Fatal(err)
			}
			result, err := doc.Exec(test.program)
			if err != nil {
				t.Fatal(err)
			}
			if got := values(t, result); fmt.Sprint(got) != fmt.Sprint(test.want) {
				t.Fatalf("values = %v, want %v", got, test.want)
			}
			if result.Changed || string(result.Bytes) != source {
				t.Fatalf("read changed the document: %s", result.Bytes)
			}
		})
	}
}

func TestVMStatements(t *testing.T) {
	source := `{"n":0,"items":[{"n":1},{"n":2}],"a":[1,2,3],"s":"x"}`
	tests := []struct {
		program string
		want    []string
		bytes   string
		changed bool
	}{
		{
			`let $count = 0; n = 1; $count`,
			[]string{"0"},
			`{"n":1,"items":[{"n":1},{"n":2}],"a":[1,2,3],"s":"x"}`,
			true,
		},
		{
			`let $x = 1; if true { let $x = 10; $x += 1; $x } $x`,
			[]string{"11", "1"},
			source,
			false,
		},
		{
			`for $item in items[] { $item.n += 1; } items`,
			[]string{`[{"n":2},{"n":3}]`},
			`{"n":0,"items":[{"n":2},{"n":3}],"a":[1,2,3],"s":"x"}`,
			true,
		},
		{
			`let $i = 0; while $i < 3 { a[$i] += 1; $i += 1 } a`,
			[]string{"[2,3,4]"},
			`{"n":0,"items":[{"n":1},{"n":2}],"a":[2,3,4],"s":"x"}`,
			true,
		},
		{
			`for $x in a[] { if $x == 2 { continue } $x += 10 } a`,
			[]string{"[11,2,13]"},
			`{"n":0,"items":[{"n":1},{"n":2}],"a":[11,2,13],"s":"x"}`,
			true,
		},
		{
			`for $x in a[] { if $x == 2 { break } $x += 10 } a`,
			[]string{"[11,2,3]"},
			`{"n":0,"items":[{"n":1},{"n":2}],"a":[11,2,3],"s":"x"}`,
			true,
		},
		{
			`new.deep[0].key = "é"; new`,
			[]string{`{"deep":[{"key":"é"}]}`},
			`{"n":0,"items":[{"n":1},{"n":2}],"a":[1,2,3],"s":"x","new":{"deep":[{"key":"é"}]}}`,
			true,
		},
		{
			`items[].missing ??= 2; items`,
			[]string{`[{"missing":2,"n":1},{"missing":2,"n":2}]`},
			`{"n":0,"items":[{"n":1,"missing":2},{"n":2,"missing":2}],"a":[1,2,3],"s":"x"}`,
			true,
		},
		{
			`a[1] += 4; a`,
			[]string{"[1,6,3]"},
			`{"n":0,"items":[{"n":1},{"n":2}],"a":[1,6,3],"s":"x"}`,
			true,
		},
		{
			`del(a[0], a[-1]); a`,
			[]string{"[2]"},
			`{"n":0,"items":[{"n":1},{"n":2}],"a":[2],"s":"x"}`,
			true,
		},
		{
			`let $copy = items; for $x in $copy[] { $x.n += 1 } $copy`,
			[]string{`[{"n":2},{"n":3}]`},
			source,
			false,
		},
		{
			`for $x in a[] { if $x == 1 { del($.a[1]) } $x += 10 } a`,
			[]string{"[11,13]"},
			`{"n":0,"items":[{"n":1},{"n":2}],"a":[11,13],"s":"x"}`,
			true,
		},
		{
			`let $keys = []; for $x in a[] { $keys += [key($x)] } $keys`,
			[]string{"[0,1,2]"},
			source,
			false,
		},
		{
			`param $delta = 2; a[] += $delta; a`,
			[]string{"[3,4,5]"},
			`{"n":0,"items":[{"n":1},{"n":2}],"a":[3,4,5],"s":"x"}`,
			true,
		},
		{
			`let $o = {x: 1}; $o.x += 2; $o`,
			[]string{`{"x":3}`},
			source,
			false,
		},
		{
			`for $x in a[] { key($x) }`,
			[]string{"0", "1", "2"},
			source,
			false,
		},
		{
			`for $x in a[] { path($x) }`,
			[]string{`["a",0]`, `["a",1]`, `["a",2]`},
			source,
			false,
		},
	}
	for _, test := range tests {
		t.Run(test.program, func(t *testing.T) {
			doc, err := Parse([]byte(source), "json")
			if err != nil {
				t.Fatal(err)
			}
			result, err := doc.Exec(test.program)
			if err != nil {
				t.Fatal(err)
			}
			if got := values(t, result); fmt.Sprint(got) != fmt.Sprint(test.want) {
				t.Fatalf("values = %v, want %v", got, test.want)
			}
			if string(result.Bytes) != test.bytes || string(doc.Bytes()) != test.bytes {
				t.Fatalf("bytes = %s, want %s", result.Bytes, test.bytes)
			}
			if result.Changed != test.changed {
				t.Fatalf("changed = %v, want %v", result.Changed, test.changed)
			}
		})
	}
}

func TestVMErrors(t *testing.T) {
	source := `{"n":0,"items":[1,2,3]}`
	tests := []struct{ program, want string }{
		{`n = 1; let $x = 2; let $x = 3`, `statement 3: variable $x is already declared in this scope`},
		{`n = 1; if true { let $hidden = 1 } n = $hidden`, `statement 3: undefined variable $hidden`},
		{`n = 1 / 0`, `statement 1: division by zero`},
		{`for $n in items[] { del($n); $n += 1 }`, `statement 1: statement 2: variable $n has no value`},
		{`n = 1; while true {}`, `statement 2: script execution limit exceeded (100000 steps)`},
		{`$a = 4; a`, `statement 1: undefined variable $a`},
		{`$unknown`, `statement 1: undefined variable $unknown`},
	}
	for _, test := range tests {
		t.Run(test.program, func(t *testing.T) {
			doc, err := Parse([]byte(source), "json")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := doc.Exec(test.program); err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if string(doc.Bytes()) != source {
				t.Fatalf("failed execution changed the document: %s", doc.Bytes())
			}
		})
	}
}
