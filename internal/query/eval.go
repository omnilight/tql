package query

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

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
func (l Literal) Eval(c, r *syntax.Node, vars ...Variables) ([]*syntax.Node, error) {
	return []*syntax.Node{Node(l.Value)}, nil
}
func (p Path) Eval(c, r *syntax.Node, vars ...Variables) ([]*syntax.Node, error) {
	ns := []*syntax.Node{c}
	if p.Root {
		ns = []*syntax.Node{r}
	}
	if p.Base != nil {
		var err error
		ns, err = p.Base.Eval(c, r, vars...)
		if err != nil {
			return nil, err
		}
	}
	for _, s := range p.Steps {
		var next []*syntax.Node
		for _, n := range ns {
			v, err := s.Eval(n, r, vars...)
			if err != nil {
				return nil, err
			}
			next = append(next, v...)
		}
		ns = next
	}
	return ns, nil
}
func (s Step) Eval(n, r *syntax.Node, vars ...Variables) ([]*syntax.Node, error) {
	if s.Kind == "dynamic" {
		resolved, err := s.Resolve(n, r, vars...)
		if err != nil {
			return nil, err
		}
		return resolved.Eval(n, r, vars...)
	}
	switch s.Kind {
	case "key":
		if n.Kind == syntax.Null {
			return nil, nil
		}
		if n.Kind != syntax.Object {
			return nil, fmt.Errorf("cannot access field %q on %s", s.Key, n.Kind)
		}
		if v := n.Get(s.Key); v != nil {
			return []*syntax.Node{v}, nil
		}
		return nil, nil
	case "index":
		if n.Kind == syntax.Null {
			return nil, nil
		}
		if n.Kind != syntax.Array {
			return nil, fmt.Errorf("cannot index %s", n.Kind)
		}
		i := s.Index
		if i < 0 {
			i += len(n.Items)
		}
		if i < 0 || i >= len(n.Items) {
			return nil, nil
		}
		return []*syntax.Node{n.Items[i]}, nil
	case "wildcard", "filter":
		var items []*syntax.Node
		switch n.Kind {
		case syntax.Array:
			items = n.Items
		case syntax.Object:
			for _, e := range n.Entries {
				items = append(items, e.Value)
			}
		default:
			return nil, fmt.Errorf("cannot iterate %s", n.Kind)
		}
		if s.Kind == "wildcard" {
			return items, nil
		}
		var out []*syntax.Node
		for _, item := range items {
			v, err := s.Predicate.Eval(item, r, vars...)
			if err != nil {
				return nil, err
			}
			for _, x := range v {
				if Truth(x.Interface()) {
					out = append(out, item)
					break
				}
			}
		}
		return out, nil
	case "slice":
		if n.Kind != syntax.Array {
			return nil, fmt.Errorf("cannot slice %s", n.Kind)
		}
		lo, hi := 0, len(n.Items)
		if s.From != nil {
			lo = bound(*s.From, hi)
		}
		if s.To != nil {
			hi = bound(*s.To, len(n.Items))
		}
		if hi < lo {
			hi = lo
		}
		return n.Items[lo:hi], nil
	}
	return nil, fmt.Errorf("unknown path step %q", s.Kind)
}
func bound(i, n int) int {
	if i < 0 {
		i += n
	}
	if i < 0 {
		return 0
	}
	if i > n {
		return n
	}
	return i
}
func One(e Expr, c, r *syntax.Node, vars ...Variables) (any, error) {
	ns, err := e.Eval(c, r, vars...)
	if err != nil {
		return nil, err
	}
	if len(ns) == 0 {
		return nil, nil
	}
	if len(ns) != 1 {
		return nil, fmt.Errorf("expression produced %d values; expected one", len(ns))
	}
	return ns[0].Interface(), nil
}
func Truth(v any) bool { return v != nil && v != false }
func (o Object) Eval(c, r *syntax.Node, vars ...Variables) ([]*syntax.Node, error) {
	m := map[string]any{}
	for i, k := range o.Keys {
		v, err := One(o.Values[i], c, r, vars...)
		if err != nil {
			return nil, err
		}
		m[k] = v
	}
	return []*syntax.Node{Node(m)}, nil
}
func (a Array) Eval(c, r *syntax.Node, vars ...Variables) ([]*syntax.Node, error) {
	vs := []any{}
	for _, e := range a.Values {
		ns, err := e.Eval(c, r, vars...)
		if err != nil {
			return nil, err
		}
		for _, n := range ns {
			vs = append(vs, n.Interface())
		}
	}
	return []*syntax.Node{Node(vs)}, nil
}
func (u Unary) Eval(c, r *syntax.Node, vars ...Variables) ([]*syntax.Node, error) {
	v, err := One(u.Expr, c, r, vars...)
	if err != nil {
		return nil, err
	}
	if u.Op == "not" || u.Op == "!" {
		return []*syntax.Node{Node(!Truth(v))}, nil
	}
	n, ok := v.(syntax.Number)
	if !ok {
		return nil, fmt.Errorf("unary %s requires number", u.Op)
	}
	if !finiteNumber.MatchString(string(n)) {
		return nil, fmt.Errorf("cannot calculate with non-finite number %q", n)
	}
	if u.Op == "-" {
		if strings.HasPrefix(string(n), "-") {
			n = syntax.Number(strings.TrimPrefix(string(n), "-"))
		} else {
			n = syntax.Number("-" + string(n))
		}
	}
	return []*syntax.Node{Node(n)}, nil
}
func (b Binary) Eval(c, r *syntax.Node, vars ...Variables) ([]*syntax.Node, error) {
	if b.Op == "|" {
		ns, err := b.Left.Eval(c, r, vars...)
		if err != nil {
			return nil, err
		}
		var out []*syntax.Node
		for _, n := range ns {
			vs, err := b.Right.Eval(n, r, vars...)
			if err != nil {
				return nil, err
			}
			out = append(out, vs...)
		}
		return out, nil
	}
	l, err := One(b.Left, c, r, vars...)
	if err != nil {
		return nil, err
	}
	if b.Op == "??" && l != nil {
		return []*syntax.Node{Node(l)}, nil
	}
	if b.Op == "and" && !Truth(l) {
		return []*syntax.Node{Node(false)}, nil
	}
	if b.Op == "or" && Truth(l) {
		return []*syntax.Node{Node(true)}, nil
	}
	rr, err := One(b.Right, c, r, vars...)
	if err != nil {
		return nil, err
	}
	v, err := Operation(b.Op, l, rr)
	if err != nil {
		return nil, err
	}
	return []*syntax.Node{Node(v)}, nil
}
