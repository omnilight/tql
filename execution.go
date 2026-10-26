package tql

import (
	"fmt"
	"strings"

	"github.com/vasileknik76/tql/internal/query"
	"github.com/vasileknik76/tql/syntax"
)

type selectionGroup struct {
	owner *binding
	nodes []*syntax.Node
	paths []address
}
type assignment struct {
	owner *binding
	path  address
	value any
}

func (d *Document) groups(ns []*syntax.Node, deleting bool) ([]selectionGroup, error) {
	groups := []selectionGroup{}
	byOwner := map[*binding]int{}
	for _, n := range ns {
		owner, e := d.runtime.owner(n)
		if e != nil {
			return nil, e
		}
		i, ok := byOwner[owner]
		if !ok {
			i = len(groups)
			byOwner[owner] = i
			groups = append(groups, selectionGroup{owner: owner})
		}
		groups[i].nodes = append(groups[i].nodes, n)
	}
	for i := range groups {
		g := &groups[i]
		as, e := uniqueTargets(d.runtime.root(g.owner), g.nodes, deleting)
		if e != nil {
			return nil, e
		}
		g.paths = as
	}
	return groups, nil
}
func (d *Document) statement(s query.Statement, res *Result) error {
	r := d.runtime
	ctx, vars := r.context(), r.variables()
	if s.Op == "read" {
		ns, err := s.Target.Eval(ctx, d.root, vars)
		if err != nil {
			return err
		}
		for _, n := range ns {
			res.Values = append(res.Values, n.Interface())
		}
		return nil
	}
	if s.Op == "del" {
		var ns []*syntax.Node
		for _, e := range s.Targets {
			nodes, err := e.Eval(ctx, d.root, vars)
			if err != nil {
				return err
			}
			ns = append(ns, nodes...)
		}
		groups, err := d.groups(ns, true)
		if err != nil {
			return err
		}
		for _, g := range groups {
			for _, a := range g.paths {
				n := locate(r.root(g.owner), a)
				if n == nil {
					return fmt.Errorf("deletion target disappeared")
				}
				if err := d.mutateOwned(g.owner, "delete", n, "", nil); err != nil {
					return err
				}
			}
		}
		return nil
	}
	var ns []*syntax.Node
	var pending []creation
	var err error
	if path, ok := s.Target.(query.Path); ok && (s.Op == "=" || s.Op == "??=") {
		bases := []*syntax.Node{ctx}
		if path.Root {
			bases = []*syntax.Node{d.root}
		}
		if path.Base != nil {
			bases, err = path.Base.Eval(ctx, d.root, vars)
			if err != nil {
				return err
			}
		}
		for _, base := range bases {
			nodes, creates, e := d.assignmentPath(base, path.Steps, vars)
			if e != nil {
				return e
			}
			ns = append(ns, nodes...)
			pending = append(pending, creates...)
		}
	} else {
		ns, err = s.Target.Eval(ctx, d.root, vars)
		if err != nil {
			return err
		}
	}
	groups, err := d.groups(ns, false)
	if err != nil {
		return err
	}
	updates := []assignment{}
	for _, g := range groups {
		for _, a := range g.paths {
			n := locate(r.root(g.owner), a)
			if s.Op == "??=" && n.Kind != syntax.Null {
				continue
			}
			rhs := ctx
			if s.Op == "|=" {
				rhs = n
			}
			v, err := query.One(s.Value, rhs, d.root, vars)
			if err != nil {
				return err
			}
			if s.Op != "=" && s.Op != "|=" && s.Op != "??=" {
				v, err = query.Operation(strings.TrimSuffix(s.Op, "="), n.Interface(), v)
				if err != nil {
					return err
				}
			}
			if !query.Equal(n.Interface(), v) {
				updates = append(updates, assignment{owner: g.owner, path: a, value: v})
			}
		}
	}
	var pendingValue any
	if len(pending) > 0 {
		pendingValue, err = query.One(s.Value, ctx, d.root, vars)
		if err != nil {
			return err
		}
	}
	if err := d.replaceMany(updates); err != nil {
		return err
	}
	for _, p := range pending {
		parent := locate(r.root(p.owner), p.parent)
		if parent == nil {
			return fmt.Errorf("insertion parent disappeared")
		}
		if err := d.createFrom(p.owner, parent, p.steps, pendingValue); err != nil {
			return err
		}
	}
	return nil
}

func (d *Document) assignmentPath(base *syntax.Node, steps []query.Step, vars query.Variables) ([]*syntax.Node, []creation, error) {
	var targets []*syntax.Node
	var pending []creation
	var walk func(*syntax.Node, []query.Step) error
	walk = func(n *syntax.Node, rest []query.Step) error {
		if len(rest) == 0 {
			targets = append(targets, n)
			return nil
		}
		st, e := rest[0].Resolve(n, d.root, vars)
		if e != nil {
			return e
		}
		missing := false
		if st.Kind == "key" {
			if n.Kind == syntax.Null {
				missing = true
			} else if n.Kind != syntax.Object {
				return fmt.Errorf("cannot create field %q on %s", st.Key, n.Kind)
			} else {
				missing = n.Get(st.Key) == nil
			}
		}
		if st.Kind == "index" {
			if n.Kind == syntax.Null {
				missing = true
			} else if n.Kind != syntax.Array {
				return fmt.Errorf("cannot index %s", n.Kind)
			} else {
				idx := st.Index
				if idx < 0 {
					idx += len(n.Items)
				}
				if idx < 0 || idx > len(n.Items) {
					return fmt.Errorf("array index %d out of bounds", st.Index)
				}
				missing = idx == len(n.Items)
			}
		}
		if missing {
			concrete := make([]query.Step, len(rest))
			for i, s := range rest {
				step, err := s.Resolve(n, d.root, vars)
				if err != nil {
					return err
				}
				if step.Kind != "key" && step.Kind != "index" {
					return nil
				}
				concrete[i] = step
			}
			owner, err := d.runtime.owner(n)
			if err != nil {
				return err
			}
			a, err := addr(d.runtime.root(owner), n)
			if err != nil {
				return err
			}
			pending = append(pending, creation{owner: owner, parent: a, steps: concrete})
			return nil
		}
		children, err := st.Eval(n, d.root, vars)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := walk(child, rest[1:]); err != nil {
				return err
			}
		}
		return nil
	}
	err := walk(base, steps)
	return targets, pending, err
}
