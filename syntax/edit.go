package syntax

import (
	"cmp"
	"fmt"
	"slices"
)

// Edit replaces [Start, End) with Text. Coordinates always refer to the same source.
type Edit struct {
	Start, End int
	Text       []byte
}

// Format implements parsing and surgical source edits. Implementations must not
// mutate source or the parsed tree. The engine reparses each independent edit
// batch; structural or overlapping edits may require sequential batches.
type Format interface {
	Parse(source []byte) (*Node, error)
	Replace(source []byte, root, target *Node, value any) ([]Edit, error)
	Insert(source []byte, root, parent *Node, key string, value any) ([]Edit, error)
	Delete(source []byte, root, target *Node) ([]Edit, error)
}

// Apply validates and applies source-relative edits without modifying its inputs.
// Edits may be unordered; insertions at the same position retain their input order.
func Apply(source []byte, edits []Edit) ([]byte, error) {
	edits = append([]Edit(nil), edits...)
	slices.SortStableFunc(edits, func(a, b Edit) int { return cmp.Compare(a.Start, b.Start) })
	pos := 0
	size := len(source)
	for _, e := range edits {
		if e.Start < pos || e.End < e.Start || e.End > len(source) {
			return nil, fmt.Errorf("invalid or overlapping edit [%d,%d)", e.Start, e.End)
		}
		size -= e.End - e.Start
		if len(e.Text) > int(^uint(0)>>1)-size {
			return nil, fmt.Errorf("edited source exceeds maximum size")
		}
		size += len(e.Text)
		pos = e.End
	}
	out := make([]byte, 0, size)
	pos = 0
	for _, e := range edits {
		out = append(out, source[pos:e.Start]...)
		out = append(out, e.Text...)
		pos = e.End
	}
	out = append(out, source[pos:]...)
	return out, nil
}
