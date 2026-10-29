package query

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestInvalidPrograms(t *testing.T) {
	for _, s := range []string{"", " ", "a =", "a b", "a[", "a[1:2.5]", "{x:1,x:2}", "01", "1.", "1e", `"\uD800"`, `"\x20"`, "a[0] = 2 a", string([]byte{0xff}), strings.Repeat("(", 300) + "1" + strings.Repeat(")", 300)} {
		if _, e := Parse(s); e == nil {
			t.Errorf("accepted invalid %q", s)
		}
	}
}
func FuzzParse(f *testing.F) {
	for _, s := range []string{`a[foo == "x"].bar = 1`, `.a[] | select(.x); del(a[0])`, `{a:[1,2],b:"\uD83D\uDE00"}`, `a ??= 3`, `a[1:3] |= . * 2`, `let $x = 1; if $x { foo = $x } else { del(foo) }`, `for $n in walk() { if type($n) == "number" { $n += 1; continue } }; while false { break }`, `obj[$key] = $value`, `param $x; $x`, `param $x = {items:[-1,2]}; $x`, `param $x = null; param $x`, `if true { param $x }`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _, _ = Parse(s) })
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
	for range 2000 {
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

func TestIntegerFastPath(t *testing.T) {
	tests := []struct {
		op   string
		a, b syntax.Number
		want syntax.Number
	}{
		{"+", "2", "3", "5"},
		{"-", "2", "3", "-1"},
		{"*", "2", "3", "6"},
		{"%", "-5", "2", "-1"},
		{"+", "9223372036854775807", "1", "9223372036854775808"},
		{"-", "9223372036854775807", "-1", "9223372036854775808"},
		{"*", "-9223372036854775808", "-1", "9223372036854775808"},
		{"+", "1.0", "2", "3"},
		{"+", "1e3", "2", "1002"},
	}
	for _, tt := range tests {
		got, err := Operation(tt.op, tt.a, tt.b)
		if err != nil {
			t.Fatalf("%s %s %s: %v", tt.a, tt.op, tt.b, err)
		}
		if got != tt.want {
			t.Fatalf("%s %s %s = %v, want %v", tt.a, tt.op, tt.b, got, tt.want)
		}
	}
	if !Equal(syntax.Number("9223372036854775807"), syntax.Number("9223372036854775807")) {
		t.Fatal("large integer equality failed")
	}
}
