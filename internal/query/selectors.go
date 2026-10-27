package query

import (
	"fmt"

	"github.com/vasileknik76/tql/syntax"
)

// SelectorStep converts a dynamic selector value into a concrete key or index
// step.
func SelectorStep(v any) (Step, error) {
	switch x := v.(type) {
	case string:
		return Step{Kind: "key", Key: x}, nil
	case syntax.Number:
		number, err := rational(x)
		if err != nil || !number.IsInt() || !number.Num().IsInt64() {
			return Step{}, fmt.Errorf("dynamic selector requires an in-range integer")
		}
		i := number.Num().Int64()
		if int64(int(i)) != i {
			return Step{}, fmt.Errorf("dynamic selector requires an in-range integer")
		}
		return Step{Kind: "index", Index: int(i)}, nil
	default:
		return Step{}, fmt.Errorf("dynamic selector requires a string or integer, got %s", KindName(v))
	}
}

func KindName(v any) string {
	switch v.(type) {
	case nil:
		return string(syntax.Null)
	case string:
		return string(syntax.String)
	case syntax.Number:
		return string(syntax.NumberKind)
	case syntax.Temporal:
		return string(syntax.Datetime)
	case bool:
		return string(syntax.Bool)
	case []any:
		return string(syntax.Array)
	case map[string]any:
		return string(syntax.Object)
	default:
		return fmt.Sprintf("%T", v)
	}
}
