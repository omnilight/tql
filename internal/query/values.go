package query

import (
	"fmt"
	"sort"

	"github.com/vasileknik76/tql/syntax"
)

// Node builds a detached syntax tree from a normalized value. Object keys are
// sorted so constructed values have a deterministic representation.
func Node(v any) *syntax.Node {
	n := node(v)
	syntax.Link(n)
	return n
}

func node(v any) *syntax.Node {
	n := &syntax.Node{Start: -1, End: -1, Value: v}
	switch x := v.(type) {
	case nil:
		n.Kind = syntax.Null
	case string:
		n.Kind = syntax.String
	case syntax.Temporal:
		n.Kind = syntax.Datetime
	case syntax.Number:
		n.Kind = syntax.NumberKind
	case bool:
		n.Kind = syntax.Bool
	case []any:
		n.Kind = syntax.Array
		n.Value = nil
		for _, v := range x {
			n.Items = append(n.Items, node(v))
		}
	case map[string]any:
		n.Kind = syntax.Object
		n.Value = nil
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			n.Entries = append(n.Entries, &syntax.Entry{Key: k, Value: node(x[k]), Start: -1, End: -1})
		}
	}
	return n
}

func Truth(v any) bool { return v != nil && v != false }

// constantValue folds the constant expression forms accepted for parameter
// defaults. It is parser work, not script execution.
func constantValue(e Expr) (any, error) {
	switch x := e.(type) {
	case Literal:
		return x.Value, nil
	case Unary:
		value, err := constantValue(x.Expr)
		if err != nil {
			return nil, err
		}
		return UnaryValue(x.Op, value)
	case Array:
		values := make([]any, 0, len(x.Values))
		for _, item := range x.Values {
			value, err := constantValue(item)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case Object:
		values := make(map[string]any, len(x.Keys))
		for i, key := range x.Keys {
			value, err := constantValue(x.Values[i])
			if err != nil {
				return nil, err
			}
			values[key] = value
		}
		return values, nil
	default:
		return nil, fmt.Errorf("parameter default must be a constant value")
	}
}
