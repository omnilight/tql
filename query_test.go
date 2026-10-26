package tql_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestSliceSignRequiresInteger(t *testing.T) {
	for _, query := range []string{`a[-:]`, `a[:-]`, `a[-:2]`, `a[1:-]`, `del(a[:-])`} {
		t.Run(query, func(t *testing.T) {
			if _, err := tql.Compile(query); err == nil {
				t.Fatal("slice sign without integer was accepted")
			}
		})
	}
}

func TestNegativeMinimumIndexAndClampedSlices(t *testing.T) {
	d := document(t, `{"a":[1,2,3]}`, "json")
	minInt := -int(^uint(0)>>1) - 1
	assertQuery(t, d, `a[`+strconv.Itoa(minInt)+`]`)
	assertQuery(t, d, `a[`+strconv.Itoa(minInt)+`:2]`, tql.Number("1"), tql.Number("2"))
}

func TestHugeDecimalEqualityAndNoOp(t *testing.T) {
	source := `{"a":1e10001,"tiny":0.1e-99999,"zero":-0e99999}`
	d := document(t, source, "json")
	assertQuery(t, d, `a == 10e10000; a > 999e9998; tiny == 1e-100000; zero == 0`, true, true, true, true)
	r := exec(t, d, `a = 10e10000; tiny = 1e-100000; zero = 0`)
	if r.Changed || string(r.Bytes) != source {
		t.Fatalf("equal huge decimals rewrote original spelling: %s", r.Bytes)
	}
}

func TestPathAndOperatorResourceLimits(t *testing.T) {
	for _, query := range []string{strings.Repeat("a.", 300) + "a=1", strings.Repeat("1+", 300) + "1"} {
		if _, err := tql.Compile(query); err == nil {
			t.Fatal("excessive expression/path depth accepted")
		}
	}
}

func TestSortRejectsSingletonUnsupportedType(t *testing.T) {
	d := document(t, `{}`, "json")
	for _, query := range []string{`sort([true])`, `sort([null])`, `sort([{x:1}])`, `sort([[]])`} {
		if _, err := d.Query(query); err == nil {
			t.Fatalf("Query(%q) accepted unsupported sort type", query)
		}
	}
}

func TestOversizedIntegerIndexIsNeverAFilter(t *testing.T) {
	for _, query := range []string{`a[9223372036854775808] = 0`, `del(a[18446744073709551616])`, `a[-9223372036854775809] = 0`} {
		t.Run(query, func(t *testing.T) {
			const source = `{"a":[1,2,3]}`
			d := document(t, source, "json")
			if _, err := d.Exec(query); err == nil {
				t.Fatal("oversized array index was treated as matching predicate")
			}
			if string(d.Bytes()) != source {
				t.Fatalf("invalid array index changed source: %s", d.Bytes())
			}
		})
	}
}
