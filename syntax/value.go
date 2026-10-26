package syntax

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Number retains a number's decimal representation without float64 rounding.
type Number string

// String returns the stored representation of n.
func (n Number) String() string { return string(n) }

// MarshalJSON preserves exact finite numbers; TOML infinities/NaNs become strings.
func (n Number) MarshalJSON() ([]byte, error) {
	s, err := JSON(n)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

var jsonNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// Temporal is a TOML date/time value. It is exposed as text by JSON output.
type Temporal string

// JSON encodes query values deterministically. It is independent of input parsers.
func JSON(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(x), nil
	case string:
		return Quote(x), nil
	case Temporal:
		return Quote(string(x)), nil
	case Number:
		if jsonNumber.MatchString(string(x)) {
			return string(x), nil
		}
		switch string(x) {
		case "inf", "+inf", "-inf", "nan", "+nan", "-nan":
			return Quote(string(x)), nil
		}
		return "", fmt.Errorf("invalid number %q", x)
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "", fmt.Errorf("non-finite float64 cannot be encoded")
		}
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	case []any:
		parts := make([]string, len(x))
		for i, v := range x {
			s, e := JSON(v)
			if e != nil {
				return "", e
			}
			parts[i] = s
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(x))
		for _, k := range keys {
			s, e := JSON(x[k])
			if e != nil {
				return "", e
			}
			parts = append(parts, Quote(k)+":"+s)
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	default:
		return "", fmt.Errorf("unsupported value %T", v)
	}
}

// Quote produces a JSON string literal (Go's quoting permits non-JSON escapes).
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
