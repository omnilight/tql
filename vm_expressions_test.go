package tql_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
)

func queryJSON(t *testing.T, d *tql.Document, query string) string {
	t.Helper()
	values, err := d.Query(query)
	if err != nil {
		t.Fatalf("Query(%q): %v", query, err)
	}
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i], err = jsonValue(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	return strings.Join(parts, ",")
}

func TestVMExpressions(t *testing.T) {
	d := document(t, `{"a":[3,1,2],"s":"café","f":false,"n":null}`, "json")
	tests := []struct{ expr, want string }{
		{`a | sort`, `[1,2,3]`}, {`a | reverse`, `[2,1,3]`}, {`a | first`, `3`}, {`a | last`, `2`}, {`s | length`, `4`},
		{`keys`, `["a","f","n","s"]`}, {`a | keys`, `[0,1,2]`}, {`f ?? true`, `false`}, {`n ?? 7`, `7`}, {`not f`, `true`},
		{`a | contains(1)`, `true`}, {`s | startswith("ca")`, `true`}, {`s | endswith("fé")`, `true`}, {`s | test("^ca")`, `true`},
		{`has("s")`, `true`}, {`has(a, 3)`, `false`}, {`length(a)`, `3`}, {`type(s)`, `"string"`}, {`[a[] | . * 2]`, `[6,2,4]`},
		{`{name: s, count: length(a)}`, `{"count":3,"name":"café"}`}, {`1 + 2 * 3`, `7`}, {`(1 + 2) * 3`, `9`},
		{`-2 + +3`, `1`}, {`10 % 3`, `1`}, {`0.1 + 0.2`, `0.3`}, {`1 / 8`, `0.125`}, {`1e3 == 1000`, `true`},
		{`[1,2] + [3]`, `[1,2,3]`}, {`"a" + "b"`, `"ab"`}, {`{a:1} + {a:2,b:3}`, `{"a":2,"b":3}`},
		{`false and (1 / 0)`, `false`}, {`true or unknown()`, `true`}, {`'it\'s'`, `"it's"`}, {`"\uD83D\uDE00"`, `"😀"`},
	}
	for _, test := range tests {
		t.Run(test.expr, func(t *testing.T) {
			if got := queryJSON(t, d, test.expr); got != test.want {
				t.Fatalf("got %s want %s", got, test.want)
			}
		})
	}
}

func TestVMExpressionErrors(t *testing.T) {
	d := document(t, `{}`, "json")
	for _, query := range []string{
		`1 / 3`, `1 / 0`, `1 % 0`, `1.1 % 1`, `"x" - 1`, `sort([1,"x"])`, `length(1)`,
		`unknown()`, `has(3)`, `test("[")`, `[1][0].foo`, `1e10001 + 1`, `1e10000 + 1`,
	} {
		t.Run(query, func(t *testing.T) {
			if _, err := d.Query(query); err == nil {
				t.Fatalf("Query(%q) succeeded", query)
			}
		})
	}
}

func TestVMNumericComparison(t *testing.T) {
	d := document(t, `{}`, "json")
	for _, test := range []struct {
		expr string
		want bool
	}{
		{`1e10001 == 10e10000`, true}, {`-0 == 0.000e999999999999999999999`, true},
		{`1e999999999999999999999 > 9e999999999999999999998`, true},
		{`-1e-999999999999999999999 < 0`, true}, {`0.00120 == 12e-4`, true},
		{`1.2 > 1.19`, true}, {`-1.2 < -1.19`, true}, {`12e-2 == 0.120`, true},
	} {
		t.Run(test.expr, func(t *testing.T) {
			values, err := d.Query(test.expr)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(values, []any{test.want}) {
				t.Fatalf("%s = %v", test.expr, values)
			}
		})
	}
	for _, query := range []string{`sort([true])`, `sort([null])`, `sort([{}])`} {
		if _, err := d.Query(query); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
	for _, query := range []string{
		`first([]) = 1`, `del(first([]))`, `[].x = 1`,
		strings.Repeat("a.", 257) + "a = 1", strings.Repeat("1+", 300) + "1",
	} {
		if _, err := tql.Compile(query); err == nil {
			t.Fatalf("accepted invalid mutation/resource limit %q", query)
		}
	}
}

func TestVMTreeFunctions(t *testing.T) {
	d := document(t, `{"z":[1,2],"a":true}`, "json")
	for _, test := range []struct{ query, want string }{
		{`[walk() | path()]`, `[["z",0],["z",1],["z"],["a"],[]]`},
		{`[walk() | key()]`, `[0,1,"z","a",null]`},
		{`[path(z[])]`, `[["z",0],["z",1]]`},
		{`[key(z[])]`, `[0,1]`},
		{`[walk(z) | path()]`, `[["z",0],["z",1],["z"]]`},
	} {
		t.Run(test.query, func(t *testing.T) {
			if got := queryJSON(t, d, test.query); got != test.want {
				t.Fatalf("got %s want %s", got, test.want)
			}
		})
	}
	for _, query := range []string{`walk(1,2)`, `path(1)`, `key([])`, `path(1,2)`, `key(1,2)`} {
		if _, err := d.Query(query); err == nil {
			t.Errorf("accepted invalid tree call %s", query)
		}
	}
}
