package query

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

func Equal(a, b any) bool {
	if x, ok := a.(syntax.Number); ok {
		y, ok := b.(syntax.Number)
		if !ok {
			return false
		}
		if xi, ok := smallInt(x); ok {
			if yi, ok := smallInt(y); ok {
				return xi == yi
			}
		}
		cmp, err := compareNumbers(x, y)
		if err == nil {
			return cmp == 0
		}
		return x == y
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case syntax.Temporal:
		y, ok := b.(syntax.Temporal)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !Equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !Equal(v, w) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}
func Operation(op string, a, b any) (any, error) {
	switch op {
	case "??":
		if a == nil {
			return b, nil
		}
		return a, nil
	case "and":
		return Truth(a) && Truth(b), nil
	case "or":
		return Truth(a) || Truth(b), nil
	case "==":
		return Equal(a, b), nil
	case "!=":
		return !Equal(a, b), nil
	case "<", "<=", ">", ">=":
		cmp, err := compare(a, b)
		if err != nil {
			return nil, err
		}
		switch op {
		case "<":
			return cmp < 0, nil
		case "<=":
			return cmp <= 0, nil
		case ">":
			return cmp > 0, nil
		default:
			return cmp >= 0, nil
		}
	case "+":
		switch x := a.(type) {
		case string:
			y, ok := b.(string)
			if ok {
				return x + y, nil
			}
		case []any:
			y, ok := b.([]any)
			if ok {
				return append(append([]any(nil), x...), y...), nil
			}
		case map[string]any:
			y, ok := b.(map[string]any)
			if ok {
				out := map[string]any{}
				for k, v := range x {
					out[k] = v
				}
				for k, v := range y {
					out[k] = v
				}
				return out, nil
			}
		}
	}
	if ai, ok := smallIntValue(a); ok {
		if bi, ok := smallIntValue(b); ok {
			if result, ok := integerOperation(op, ai, bi); ok {
				return result, nil
			}
		}
	}
	ar, err := rational(a)
	if err != nil {
		return nil, fmt.Errorf("operator %s: %w", op, err)
	}
	br, err := rational(b)
	if err != nil {
		return nil, fmt.Errorf("operator %s: %w", op, err)
	}
	switch op {
	case "+":
		ar.Add(ar, br)
	case "-":
		ar.Sub(ar, br)
	case "*":
		ar.Mul(ar, br)
	case "/":
		if br.Sign() == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		ar.Quo(ar, br)
	case "%":
		if !ar.IsInt() || !br.IsInt() {
			return nil, fmt.Errorf("remainder requires integers")
		}
		if br.Sign() == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		ar.SetInt(new(big.Int).Rem(ar.Num(), br.Num()))
	default:
		return nil, fmt.Errorf("unknown operator %q", op)
	}
	return decimal(ar)
}
func compare(a, b any) (int, error) {
	if x, ok := a.(string); ok {
		y, ok := b.(string)
		if ok {
			return strings.Compare(x, y), nil
		}
	}
	an, ok := a.(syntax.Number)
	bn, ok2 := b.(syntax.Number)
	if !ok || !ok2 {
		return 0, fmt.Errorf("comparison requires two strings or two numbers")
	}
	return compareNumbers(an, bn)
}
func rational(v any) (*big.Rat, error) {
	n, ok := v.(syntax.Number)
	if !ok {
		return nil, fmt.Errorf("expected number, got %T", v)
	}
	s := string(n)
	if len(s) > 10000 {
		return nil, fmt.Errorf("number exceeds arithmetic precision limit")
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exp, err := strconv.Atoi(s[i+1:])
		if err != nil || exp > 10000 || exp < -10000 {
			return nil, fmt.Errorf("number exponent exceeds arithmetic limit")
		}
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("cannot calculate with non-finite number %q", s)
	}
	return r, nil
}
func decimal(r *big.Rat) (syntax.Number, error) {
	if r.Num().BitLen() > 40000 || r.Denom().BitLen() > 40000 {
		return "", fmt.Errorf("arithmetic result exceeds precision limit")
	}
	d := new(big.Int).Set(r.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	rem := new(big.Int)
	a, b := 0, 0
	for {
		q := new(big.Int)
		q.QuoRem(d, two, rem)
		if rem.Sign() != 0 {
			break
		}
		d = q
		a++
	}
	for {
		q := new(big.Int)
		q.QuoRem(d, five, rem)
		if rem.Sign() != 0 {
			break
		}
		d = q
		b++
	}
	if d.Cmp(big.NewInt(1)) != 0 {
		return "", fmt.Errorf("division has no finite decimal representation")
	}
	if b > a {
		a = b
	}
	if a > 10000 {
		return "", fmt.Errorf("arithmetic result exceeds precision limit")
	}
	s := r.FloatString(a)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	digits := len(strings.ReplaceAll(strings.TrimPrefix(s, "-"), ".", ""))
	if digits > 10000 {
		return "", fmt.Errorf("arithmetic result exceeds precision limit")
	}
	return syntax.Number(s), nil
}

// Canonical decimal comparison never constructs 10^exponent. It remains exact
// even when input exponents exceed the arithmetic precision budget.
var finiteNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

type canonical struct {
	negative bool
	digits   string
	scale    *big.Int
}

func canonicalNumber(n syntax.Number) (canonical, error) {
	s := string(n)
	if !finiteNumber.MatchString(s) {
		return canonical{}, fmt.Errorf("cannot compare non-finite number %q", s)
	}
	c := canonical{scale: new(big.Int)}
	if s[0] == '-' {
		c.negative = true
		s = s[1:]
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		c.scale.SetString(s[i+1:], 10)
		s = s[:i]
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		c.scale.Sub(c.scale, big.NewInt(int64(len(s)-i-1)))
		s = s[:i] + s[i+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		c.digits = "0"
		c.negative = false
		c.scale.SetInt64(0)
		return c, nil
	}
	trimmed := strings.TrimRight(s, "0")
	c.scale.Add(c.scale, big.NewInt(int64(len(s)-len(trimmed))))
	c.digits = trimmed
	return c, nil
}
func compareNumbers(a, b syntax.Number) (int, error) {
	if ai, ok := smallInt(a); ok {
		if bi, ok := smallInt(b); ok {
			switch {
			case ai < bi:
				return -1, nil
			case ai > bi:
				return 1, nil
			default:
				return 0, nil
			}
		}
	}
	x, e := canonicalNumber(a)
	if e != nil {
		return 0, e
	}
	y, e := canonicalNumber(b)
	if e != nil {
		return 0, e
	}
	if x.negative != y.negative {
		if x.negative {
			return -1, nil
		}
		return 1, nil
	}
	sign := 1
	if x.negative {
		sign = -1
	}
	if x.digits == "0" {
		if y.digits == "0" {
			return 0, nil
		}
		return -sign, nil
	}
	if y.digits == "0" {
		return sign, nil
	}
	ax := new(big.Int).Add(x.scale, big.NewInt(int64(len(x.digits))))
	ay := new(big.Int).Add(y.scale, big.NewInt(int64(len(y.digits))))
	if cmp := ax.Cmp(ay); cmp != 0 {
		return sign * cmp, nil
	}
	n := len(x.digits)
	if len(y.digits) > n {
		n = len(y.digits)
	}
	for i := 0; i < n; i++ {
		ca, cb := byte('0'), byte('0')
		if i < len(x.digits) {
			ca = x.digits[i]
		}
		if i < len(y.digits) {
			cb = y.digits[i]
		}
		if ca < cb {
			return -sign, nil
		}
		if ca > cb {
			return sign, nil
		}
	}
	return 0, nil
}

// smallInt accepts canonical integer spellings that fit int64. Decimal,
// exponent and non-canonical spellings use the exact rational path.
func smallInt(n syntax.Number) (int64, bool) {
	s := string(n)
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || strconv.FormatInt(v, 10) != s {
		return 0, false
	}
	return v, true
}

func smallIntValue(v any) (int64, bool) {
	n, ok := v.(syntax.Number)
	if !ok {
		return 0, false
	}
	return smallInt(n)
}

func integerOperation(op string, a, b int64) (syntax.Number, bool) {
	switch op {
	case "+":
		c := a + b
		if (b > 0 && c < a) || (b < 0 && c > a) {
			return "", false
		}
		return syntax.Number(strconv.FormatInt(c, 10)), true
	case "-":
		if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
			return "", false
		}
		return syntax.Number(strconv.FormatInt(a-b, 10)), true
	case "*":
		if a == 0 || b == 0 {
			return "0", true
		}
		if a == -1<<63 && b == -1 || b == -1<<63 && a == -1 {
			return "", false
		}
		c := a * b
		if c/b != a {
			return "", false
		}
		return syntax.Number(strconv.FormatInt(c, 10)), true
	case "%":
		if b == 0 {
			return "", false
		}
		return syntax.Number(strconv.FormatInt(a%b, 10)), true
	default:
		return "", false
	}
}
