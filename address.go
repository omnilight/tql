package tql

import (
	"fmt"
	"sort"

	"github.com/vasileknik76/tql/syntax"
)

type address []segment
type segment struct {
	key   string
	index int
	array bool
}

func locate(root *syntax.Node, a address) *syntax.Node {
	n := root
	for _, s := range a {
		if n == nil {
			return nil
		}
		if s.array {
			if n.Kind != syntax.Array || s.index < 0 || s.index >= len(n.Items) {
				return nil
			}
			n = n.Items[s.index]
		} else {
			if n.Kind != syntax.Object {
				return nil
			}
			n = n.Get(s.key)
		}
	}
	return n
}
func addr(root, n *syntax.Node) (address, error) {
	if n == root {
		return address{}, nil
	}
	var rev address
	for n != root {
		p := n.Parent
		if p == nil {
			return nil, fmt.Errorf("assignment target must select nodes from the document")
		}
		if p.Kind == syntax.Array {
			i := p.Index(n)
			if i < 0 {
				return nil, fmt.Errorf("invalid tree parent")
			}
			rev = append(rev, segment{array: true, index: i})
		} else {
			e := p.Entry(n)
			if e == nil {
				return nil, fmt.Errorf("invalid tree parent")
			}
			rev = append(rev, segment{key: e.Key})
		}
		n = p
	}
	a := make(address, len(rev))
	for i := range rev {
		a[len(rev)-1-i] = rev[i]
	}
	return a, nil
}
func prefix(a, b address) bool {
	if len(a) > len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func compareAddress(a, b address) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		x, y := a[i], b[i]
		if x == y {
			continue
		}
		if x.array != y.array {
			if x.array {
				return 1
			}
			return -1
		}
		if x.array {
			if x.index < y.index {
				return -1
			}
			return 1
		}
		if x.key < y.key {
			return -1
		}
		return 1
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}
func uniqueTargets(root *syntax.Node, ns []*syntax.Node, deleting bool) ([]address, error) {
	paths := make([]address, 0, len(ns))
	seen := make(map[*syntax.Node]struct{}, len(ns))
	for _, n := range ns {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		a, err := addr(root, n)
		if err != nil {
			return nil, err
		}
		paths = append(paths, a)
	}
	sort.Slice(paths, func(i, j int) bool { return compareAddress(paths[i], paths[j]) < 0 })
	kept := paths[:0]
	for _, a := range paths {
		if len(kept) > 0 {
			previous := kept[len(kept)-1]
			if prefix(previous, a) {
				if len(previous) == len(a) {
					continue
				}
				if !deleting {
					return nil, fmt.Errorf("overlapping assignment targets")
				}
				continue
			}
		}
		kept = append(kept, a)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept, nil
}
