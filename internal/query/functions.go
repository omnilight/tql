package query

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vasileknik76/tql/syntax"
)

func (f Call) Eval(c, r *syntax.Node, vars ...Variables) ([]*syntax.Node, error) {
	if f.Name == "walk" || f.Name == "path" || f.Name == "key" {
		return f.treeEval(c, r, vars...)
	}
	if f.Name == "select" {
		if len(f.Args) != 1 {
			return nil, fmt.Errorf("select expects one predicate")
		}
		v, err := One(f.Args[0], c, r, vars...)
		if err != nil {
			return nil, err
		}
		if Truth(v) {
			return []*syntax.Node{c}, nil
		}
		return nil, nil
	}
	var v any
	var err error
	// Zero-argument aggregate functions operate on current input; one argument is explicit input.
	switch f.Name {
	case "length", "keys", "values", "type", "sort", "reverse", "first", "last":
		if len(f.Args) > 1 {
			return nil, fmt.Errorf("%s expects zero or one argument", f.Name)
		}
		input := c
		if len(f.Args) == 1 {
			nodes, evalErr := f.Args[0].Eval(c, r, vars...)
			if evalErr != nil {
				return nil, evalErr
			}
			if len(nodes) > 1 {
				return nil, fmt.Errorf("expression produced %d values; expected one", len(nodes))
			}
			input = Node(nil)
			if len(nodes) == 1 {
				input = nodes[0]
			}
		}
		// Inspect tree metadata directly: type()/length() are frequent in tree
		// walks and converting every subtree to Interface would be quadratic.
		if f.Name == "type" {
			return []*syntax.Node{Node(string(input.Kind))}, nil
		}
		if f.Name == "length" {
			length := 0
			switch input.Kind {
			case syntax.Object:
				length = len(input.Entries)
			case syntax.Array:
				length = len(input.Items)
			case syntax.String:
				length = utf8.RuneCountInString(input.Value.(string))
			case syntax.Null:
			default:
				return nil, fmt.Errorf("length requires string, array or object")
			}
			return []*syntax.Node{Node(syntax.Number(strconv.Itoa(length)))}, nil
		}
		v = input.Interface()
		var out any
		switch f.Name {
		case "keys":
			a := []any{}
			switch x := v.(type) {
			case map[string]any:
				keys := []string{}
				for k := range x {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					a = append(a, k)
				}
			case []any:
				for i := range x {
					a = append(a, syntax.Number(strconv.Itoa(i)))
				}
			default:
				return nil, fmt.Errorf("keys requires object or array")
			}
			out = a
		case "values":
			x, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("values requires object")
			}
			ks := []string{}
			for k := range x {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			a := []any{}
			for _, k := range ks {
				a = append(a, x[k])
			}
			out = a
		default:
			x, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("%s requires array", f.Name)
			}
			a := append([]any{}, x...)
			switch f.Name {
			case "first":
				if len(a) == 0 {
					return nil, nil
				}
				out = a[0]
			case "last":
				if len(a) == 0 {
					return nil, nil
				}
				out = a[len(a)-1]
			case "reverse":
				for i, j := 0, len(a)-1; i < j; i, j = i+1, j-1 {
					a[i], a[j] = a[j], a[i]
				}
				out = a
			case "sort":
				for _, item := range a {
					if _, e := compare(item, item); e != nil {
						return nil, e
					}
				}
				var sortErr error
				sort.SliceStable(a, func(i, j int) bool {
					cmp, e := compare(a[i], a[j])
					if e != nil {
						sortErr = e
					}
					return cmp < 0
				})
				if sortErr != nil {
					return nil, sortErr
				}
				out = a
			}
		}
		return []*syntax.Node{Node(out)}, nil
	case "has", "contains", "startswith", "endswith", "test":
		if len(f.Args) != 1 && len(f.Args) != 2 {
			return nil, fmt.Errorf("%s expects one argument (or input, argument)", f.Name)
		}
		input := c.Interface()
		argIndex := 0
		if len(f.Args) == 2 {
			input, err = One(f.Args[0], c, r, vars...)
			if err != nil {
				return nil, err
			}
			argIndex = 1
		}
		arg, err := One(f.Args[argIndex], c, r, vars...)
		if err != nil {
			return nil, err
		}
		var result bool
		switch f.Name {
		case "has":
			switch x := input.(type) {
			case map[string]any:
				k, ok := arg.(string)
				if !ok {
					return nil, fmt.Errorf("has expects string key")
				}
				_, result = x[k]
			case []any:
				k, ok := arg.(syntax.Number)
				if !ok {
					return nil, fmt.Errorf("has expects integer index")
				}
				i, er := strconv.Atoi(string(k))
				if er != nil {
					return nil, fmt.Errorf("has expects integer index")
				}
				result = i >= 0 && i < len(x)
			default:
				return nil, fmt.Errorf("has requires object or array")
			}
		case "contains":
			switch x := input.(type) {
			case string:
				y, ok := arg.(string)
				if !ok {
					return nil, fmt.Errorf("string contains expects string")
				}
				result = strings.Contains(x, y)
			case []any:
				for _, v := range x {
					if Equal(v, arg) {
						result = true
						break
					}
				}
			default:
				return nil, fmt.Errorf("contains requires string or array")
			}
		default:
			x, ok := input.(string)
			y, ok2 := arg.(string)
			if !ok || !ok2 {
				return nil, fmt.Errorf("%s requires strings", f.Name)
			}
			switch f.Name {
			case "startswith":
				result = strings.HasPrefix(x, y)
			case "endswith":
				result = strings.HasSuffix(x, y)
			case "test":
				re, er := regexp.Compile(y)
				if er != nil {
					return nil, er
				}
				result = re.MatchString(x)
			}
		}
		return []*syntax.Node{Node(result)}, nil
	default:
		return nil, fmt.Errorf("unknown function %q", f.Name)
	}
}
