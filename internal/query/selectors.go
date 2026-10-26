package query

import (
	"fmt"
	"strconv"

	"github.com/vasileknik76/tql/syntax"
)

func (v Variable) Eval(c, r *syntax.Node, vars ...Variables) ([]*syntax.Node, error) {
	for i := len(vars) - 1; i >= 0; i-- {
		if n, ok := vars[i][v.Name]; ok {
			if n == nil {
				return nil, fmt.Errorf("variable $%s has no value", v.Name)
			}
			return []*syntax.Node{n}, nil
		}
	}
	return nil, fmt.Errorf("undefined variable $%s", v.Name)
}

// Resolve evaluates a dynamic selector without changing its expression AST.
// Concrete selectors are returned unchanged, allowing creation and reads to use
// the same key/index rules.
func (s Step) Resolve(c, r *syntax.Node, vars ...Variables) (Step, error) {
	if s.Kind != "dynamic" {
		return s, nil
	}
	ns, err := s.Predicate.Eval(c, r, vars...)
	if err != nil {
		return Step{}, err
	}
	if len(ns) != 1 {
		return Step{}, fmt.Errorf("dynamic selector requires exactly one string or integer")
	}
	switch v := ns[0].Interface().(type) {
	case string:
		return Step{Kind: "key", Key: v}, nil
	case syntax.Number:
		number, err := rational(v)
		if err != nil || !number.IsInt() || !number.Num().IsInt64() {
			return Step{}, fmt.Errorf("dynamic selector requires an in-range integer")
		}
		i := number.Num().Int64()
		if int64(int(i)) != i {
			return Step{}, fmt.Errorf("dynamic selector requires an in-range integer")
		}
		return Step{Kind: "index", Index: int(i)}, nil
	default:
		return Step{}, fmt.Errorf("dynamic selector requires a string or integer, got %s", ns[0].Kind)
	}
}

func (f Call) treeEval(c, r *syntax.Node, vars ...Variables) ([]*syntax.Node, error) {
	if len(f.Args) > 1 {
		return nil, fmt.Errorf("%s expects zero or one argument", f.Name)
	}
	ns := []*syntax.Node{c}
	if len(f.Args) == 1 {
		var err error
		ns, err = f.Args[0].Eval(c, r, vars...)
		if err != nil {
			return nil, err
		}
	}
	var out []*syntax.Node
	for _, n := range ns {
		if f.Name == "walk" {
			// Explicit stack avoids recursion and preserves source order in postorder.
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
		for node != r {
			parent := node.Parent
			if parent == nil {
				return nil, fmt.Errorf("%s requires a node in the document", f.Name)
			}
			if parent.Kind == syntax.Object {
				entry := parent.Entry(node)
				if entry == nil {
					return nil, fmt.Errorf("%s: node is detached from its parent", f.Name)
				}
				path = append(path, entry.Key)
			} else {
				i := parent.Index(node)
				if i < 0 {
					return nil, fmt.Errorf("%s: node is detached from its parent", f.Name)
				}
				path = append(path, syntax.Number(strconv.Itoa(i)))
			}
			node = parent
		}
		if f.Name == "key" {
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
