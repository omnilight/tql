package query

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vasileknik76/tql/internal/scan"
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
				length = scan.RuneCount(input.Value.(string))
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

// ApplyBuiltin applies a non-tree built-in to already evaluated argument nodes.
// It is shared by the bytecode VM; the AST evaluator keeps its own argument
// acquisition rules but must produce the same results.
func ApplyBuiltin(name string, ctx *syntax.Node, args []*syntax.Node) ([]*syntax.Node, error) {
	switch name {
	case "length", "keys", "values", "type", "sort", "reverse", "first", "last":
		if len(args) > 1 {
			return nil, fmt.Errorf("%s expects zero or one argument", name)
		}
		input := ctx
		if len(args) == 1 {
			input = args[0]
		}
		if name == "type" {
			return []*syntax.Node{Node(string(input.Kind))}, nil
		}
		if name == "length" {
			length := 0
			switch input.Kind {
			case syntax.Object:
				length = len(input.Entries)
			case syntax.Array:
				length = len(input.Items)
			case syntax.String:
				length = scan.RuneCount(input.Value.(string))
			case syntax.Null:
			default:
				return nil, fmt.Errorf("length requires string, array or object")
			}
			return []*syntax.Node{Node(syntax.Number(strconv.Itoa(length)))}, nil
		}
		v := input.Interface()
		var out any
		switch name {
		case "keys":
			a := []any{}
			switch x := v.(type) {
			case map[string]any:
				keys := make([]string, 0, len(x))
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
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			a := []any{}
			for _, k := range keys {
				a = append(a, x[k])
			}
			out = a
		default:
			x, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("%s requires array", name)
			}
			a := append([]any{}, x...)
			switch name {
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
					if _, err := compare(item, item); err != nil {
						return nil, err
					}
				}
				var sortErr error
				sort.SliceStable(a, func(i, j int) bool {
					cmp, err := compare(a[i], a[j])
					if err != nil {
						sortErr = err
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
		if len(args) != 1 && len(args) != 2 {
			return nil, fmt.Errorf("%s expects one argument (or input, argument)", name)
		}
		input := ctx.Interface()
		argIndex := 0
		if len(args) == 2 {
			input = args[0].Interface()
			argIndex = 1
		}
		arg := args[argIndex].Interface()
		var result bool
		switch name {
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
				i, err := strconv.Atoi(string(k))
				if err != nil {
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
				return nil, fmt.Errorf("%s requires strings", name)
			}
			switch name {
			case "startswith":
				result = strings.HasPrefix(x, y)
			case "endswith":
				result = strings.HasSuffix(x, y)
			case "test":
				re, err := regexp.Compile(y)
				if err != nil {
					return nil, err
				}
				result = re.MatchString(x)
			}
		}
		return []*syntax.Node{Node(result)}, nil
	case "select":
		return nil, fmt.Errorf("select expects one predicate")
	default:
		return nil, fmt.Errorf("unknown function %q", name)
	}
}

// ApplyTreeFunction applies walk, path or key to an already evaluated argument
// stream. hasArg distinguishes walk() from walk(empty-expression).
func ApplyTreeFunction(name string, ctx, root *syntax.Node, arg []*syntax.Node, hasArg bool) ([]*syntax.Node, error) {
	ns := []*syntax.Node{ctx}
	if hasArg {
		ns = arg
	}
	var out []*syntax.Node
	for _, n := range ns {
		if name == "walk" {
			type frame struct {
				n       *syntax.Node
				visited bool
			}
			stack := []frame{{n: n}}
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.visited {
					out = append(out, top.n)
					continue
				}
				stack = append(stack, frame{n: top.n, visited: true})
				for i := len(top.n.Items) - 1; i >= 0; i-- {
					stack = append(stack, frame{n: top.n.Items[i]})
				}
				for i := len(top.n.Entries) - 1; i >= 0; i-- {
					stack = append(stack, frame{n: top.n.Entries[i].Value})
				}
			}
			continue
		}
		path := []any{}
		node := n
		for node != root {
			parent := node.Parent
			if parent == nil {
				return nil, fmt.Errorf("%s requires a node in the document", name)
			}
			if parent.Kind == syntax.Object {
				entry := parent.Entry(node)
				if entry == nil {
					return nil, fmt.Errorf("%s: node is detached from its parent", name)
				}
				path = append(path, entry.Key)
			} else {
				i := parent.Index(node)
				if i < 0 {
					return nil, fmt.Errorf("%s: node is detached from its parent", name)
				}
				path = append(path, syntax.Number(strconv.Itoa(i)))
			}
			node = parent
		}
		if name == "key" {
			var key any
			if len(path) > 0 {
				key = path[0]
			}
			out = append(out, Node(key))
		} else {
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			out = append(out, Node(path))
		}
	}
	return out, nil
}

// UnaryValue applies a unary operator to one evaluated value.
func UnaryValue(op string, v any) (any, error) {
	if op == "not" || op == "!" {
		return !Truth(v), nil
	}
	n, ok := v.(syntax.Number)
	if !ok {
		return nil, fmt.Errorf("unary %s requires number", op)
	}
	if !finiteNumber.MatchString(string(n)) {
		return nil, fmt.Errorf("cannot calculate with non-finite number %q", n)
	}
	if op == "-" {
		if strings.HasPrefix(string(n), "-") {
			n = syntax.Number(strings.TrimPrefix(string(n), "-"))
		} else {
			n = syntax.Number("-" + string(n))
		}
	}
	return n, nil
}
