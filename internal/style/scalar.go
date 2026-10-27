// Package style contains value-safe scalar presentation shared by backends.
package style

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

func Kind(value any) syntax.Kind {
	switch value.(type) {
	case map[string]any:
		return syntax.Object
	case []any:
		return syntax.Array
	case string:
		return syntax.String
	case nil:
		return syntax.Null
	case bool:
		return syntax.Bool
	case int, int64, float64, syntax.Number:
		return syntax.NumberKind
	case syntax.Temporal:
		return syntax.Datetime
	}
	return ""
}

// Integer changes only integral decimal values. Floats, negative base-prefixed
// integers and special values keep the backend's exact, safe representation.
func Integer(value any, policy, sample string) (string, bool) {
	var text string
	switch v := value.(type) {
	case int:
		text = strconv.Itoa(v)
	case int64:
		text = strconv.FormatInt(v, 10)
	case syntax.Number:
		text = string(v)
	default:
		return "", false
	}
	z, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return "", false
	}
	inferred := policy == "" || policy == "auto"
	if inferred {
		policy = "decimal"
		switch {
		case strings.HasPrefix(sample, "0x"):
			policy = "hex"
		case strings.HasPrefix(sample, "0o"):
			policy = "octal"
		case strings.HasPrefix(sample, "0b"):
			policy = "binary"
		}
	}
	base, prefix := 10, ""
	switch policy {
	case "hex":
		base, prefix = 16, "0x"
	case "octal":
		base, prefix = 8, "0o"
	case "binary":
		base, prefix = 2, "0b"
	}
	if z.Sign() < 0 && base != 10 {
		return "", false
	}
	out := z.Text(base)
	if inferred && base == 16 && strings.ContainsAny(sample, "ABCDEF") && !strings.ContainsAny(sample, "abcdef") {
		out = strings.ToUpper(out)
	}
	return prefix + out, true
}

// Signature groups lexical representations, never scalar contents.
func Signature(src []byte, n *syntax.Node) string {
	if n.Start < 0 || n.End > len(src) {
		return n.Style
	}
	text := string(src[n.Start:n.End])
	switch n.Kind {
	case syntax.String:
		switch {
		case strings.HasPrefix(text, "'''"):
			return "literal"
		case strings.HasPrefix(text, `"""`):
			return "multiline"
		case strings.HasPrefix(text, "'"):
			return "single"
		case strings.HasPrefix(text, `"`):
			return "double"
		}
	case syntax.NumberKind:
		switch {
		case strings.HasPrefix(text, "0x"):
			return "hex"
		case strings.HasPrefix(text, "0o"):
			return "octal"
		case strings.HasPrefix(text, "0b"):
			return "binary"
		}
		return "decimal"
	case syntax.Array, syntax.Object:
		if strings.ContainsAny(text, "\r\n") {
			return n.Style + "-multiline"
		}
	}
	return n.Style
}

func ObjectArray(value any) bool {
	xs, ok := value.([]any)
	if !ok || len(xs) == 0 {
		return false
	}
	for _, v := range xs {
		if _, ok := v.(map[string]any); !ok {
			return false
		}
	}
	return true
}
func ObjectArrayNode(n *syntax.Node) bool {
	if n.Kind != syntax.Array || len(n.Items) == 0 {
		return false
	}
	for _, v := range n.Items {
		if v.Kind != syntax.Object {
			return false
		}
	}
	return true
}
