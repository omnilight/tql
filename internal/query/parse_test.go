package query

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestExpressions(t *testing.T) {
	root := Node(map[string]any{"a": []any{syntax.Number("3"), syntax.Number("1"), syntax.Number("2")}, "s": "café", "f": false, "n": nil})
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
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			p, e := Parse(tt.expr)
			if e != nil {
				t.Fatal(e)
			}
			v, e := One(p.Statements[0].Target, root, root)
			if e != nil {
				t.Fatal(e)
			}
			got, e := syntax.JSON(v)
			if e != nil {
				t.Fatal(e)
			}
			if got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}
func TestInvalidPrograms(t *testing.T) {
	for _, s := range []string{"", " ", "a =", "a b", "a[", "a[1:2.5]", "{x:1,x:2}", "01", "1.", "1e", `"\uD800"`, `"\x20"`, "a[0] = 2 a", string([]byte{0xff}), strings.Repeat("(", 300) + "1" + strings.Repeat(")", 300)} {
		if _, e := Parse(s); e == nil {
			t.Errorf("accepted invalid %q", s)
		}
	}
}
func TestExpressionErrors(t *testing.T) {
	root := Node(map[string]any{})
	for _, s := range []string{`1 / 3`, `1 / 0`, `1 % 0`, `1.1 % 1`, `"x" - 1`, `sort([1,"x"])`, `length(1)`, `unknown()`, `has(3)`, `test("[")`, `[1][0].foo`, `1e10001 + 1`, `1e10000 + 1`} {
		p, e := Parse(s)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Statements[0].Target.Eval(root, root); e == nil {
			t.Errorf("expected error: %s", s)
		}
	}
}
func FuzzParse(f *testing.F) {
	for _, s := range []string{`a[foo == "x"].bar = 1`, `.a[] | select(.x); del(a[0])`, `{a:[1,2],b:"\uD83D\uDE00"}`, `a ??= 3`, `a[1:3] |= . * 2`, `let $x = 1; if $x { foo = $x } else { del(foo) }`, `for $n in walk() { if type($n) == "number" { $n += 1; continue } }; while false { break }`, `obj[$key] = $value`, `param $x; $x`, `param $x = {items:[-1,2]}; $x`, `param $x = null; param $x`, `if true { param $x }`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _, _ = Parse(s) })
}

func TestCanonicalNumericComparison(t *testing.T) {
	root := Node(nil)
	for _, tc := range []struct {
		expr string
		want bool
	}{
		{`1e10001 == 10e10000`, true}, {`-0 == 0.000e999999999999999999999`, true},
		{`1e999999999999999999999 > 9e999999999999999999998`, true},
		{`-1e-999999999999999999999 < 0`, true}, {`0.00120 == 12e-4`, true},
		{`1.2 > 1.19`, true}, {`-1.2 < -1.19`, true}, {`12e-2 == 0.120`, true},
	} {
		p, e := Parse(tc.expr)
		if e != nil {
			t.Fatal(e)
		}
		v, e := One(p.Statements[0].Target, root, root)
		if e != nil {
			t.Fatal(e)
		}
		if v != tc.want {
			t.Errorf("%s = %v", tc.expr, v)
		}
	}
	for _, s := range []string{`sort([true])`, `sort([null])`, `sort([{}])`} {
		p, e := Parse(s)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := One(p.Statements[0].Target, root, root); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	for _, s := range []string{`first([]) = 1`, `del(first([]))`, `[].x = 1`, strings.Repeat("a.", 257) + "a = 1", strings.Repeat("1+", 300) + "1"} {
		if _, e := Parse(s); e == nil {
			t.Fatalf("accepted invalid mutation/resource limit %q", s)
		}
	}
}

func TestNumericComparisonOracle(t *testing.T) {
	// Independent big.Rat oracle for signs, fractions and decimal alignment.
	state := uint64(8675309)
	next := func() uint64 { state = state*6364136223846793005 + 1; return state }
	number := func() syntax.Number {
		sign := ""
		if next()%2 != 0 {
			sign = "-"
		}
		whole := next() % 100000
		fraction := next() % 100000
		exponent := int(next()%61) - 30
		return syntax.Number(fmt.Sprintf("%s%d.%05de%d", sign, whole, fraction, exponent))
	}
	for i := 0; i < 2000; i++ {
		a, b := number(), number()
		x, ok := new(big.Rat).SetString(string(a))
		if !ok {
			t.Fatal(a)
		}
		y, ok := new(big.Rat).SetString(string(b))
		if !ok {
			t.Fatal(b)
		}
		got, e := compareNumbers(a, b)
		if e != nil {
			t.Fatal(e)
		}
		if got != x.Cmp(y) {
			t.Fatalf("compare(%s,%s)=%d want %d", a, b, got, x.Cmp(y))
		}
	}
}
