package tql

import (
	"slices"

	"github.com/vasileknik76/tql/internal/query"
	"github.com/vasileknik76/tql/syntax"
)

// expectedChange builds an unlinked verification tree, copying only ancestors of
// the mutation. The source tree stays immutable, including its parent pointers
// and indexes. This tree is used only for full semantic comparison, never lookup
// by parent or source range, and must not be linked (it shares untouched children).
func expectedChange(n *syntax.Node, op string, path address, key string, value any) *syntax.Node {
	if len(path) == 0 && op == "delete" {
		return query.Node(nil)
	}
	if len(path) == 0 && op == "replace" {
		return query.Node(value)
	}
	out := &syntax.Node{Kind: n.Kind, Value: n.Value}
	if n.Kind == syntax.Object {
		out.Entries = slices.Clone(n.Entries)
	} else {
		out.Items = slices.Clone(n.Items)
	}
	if len(path) == 0 {
		if n.Kind == syntax.Object {
			out.Entries = append(out.Entries, &syntax.Entry{Key: key, Value: query.Node(value)})
		} else {
			out.Items = append(out.Items, query.Node(value))
		}
		return out
	}
	step := path[0]
	if step.array {
		if len(path) == 1 && op == "delete" {
			out.Items = slices.Delete(out.Items, step.index, step.index+1)
		} else {
			out.Items[step.index] = expectedChange(n.Items[step.index], op, path[1:], key, value)
		}
	} else {
		for i, entry := range n.Entries {
			if entry.Key != step.key {
				continue
			}
			if len(path) == 1 && op == "delete" {
				out.Entries = slices.Delete(out.Entries, i, i+1)
			} else {
				copyEntry := *entry
				copyEntry.Value = expectedChange(entry.Value, op, path[1:], key, value)
				out.Entries[i] = &copyEntry
			}
			break
		}
	}
	return out
}
