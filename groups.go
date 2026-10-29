package tql

import (
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
		owner, e := d.state.owner(n)
		if e != nil {
			return nil, e
		}
		if err := writable(owner); err != nil {
			return nil, err
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
		as, e := uniqueTargets(d.state.root(g.owner), g.nodes, deleting)
		if e != nil {
			return nil, e
		}
		g.paths = as
	}
	return groups, nil
}
