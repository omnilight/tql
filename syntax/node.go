// Package syntax defines the source-aware tree and edit protocol used by TQL formats.
package syntax

// Kind identifies the semantic type of a node.
type Kind string

const (
	Object     Kind = "object"
	Array      Kind = "array"
	String     Kind = "string"
	NumberKind Kind = "number"
	Bool       Kind = "boolean"
	Null       Kind = "null"
	Datetime   Kind = "datetime"
)

// Node spans [Start, End) in the original UTF-8 byte stream. Synthetic containers
// (for example an implicit TOML table) may have Start == -1. Meta is format-owned.
// Parsers may build a Node incrementally; call Link after construction. Linked
// trees must remain immutable during evaluation, including Entries and Items.
type Node struct {
	Kind       Kind
	Value      any
	Start, End int
	Entries    []*Entry
	Items      []*Node
	Parent     *Node
	Style      string
	Meta       map[string]any
	index      *nodeIndex
}

// Small containers are cheaper to scan than to index. A single optional pointer
// keeps the overhead on scalar nodes low; wide containers get constant-time key
// and reverse-child lookup without lazy writes during query evaluation.
const indexThreshold = 32

type nodeIndex struct {
	keys     map[string]*Node
	children map[*Node]int
}

// Entry associates a decoded object key with its spelling and value in the source.
// Start/End cover the key/value syntax; KeyStart/KeyEnd cover only the key.
type Entry struct {
	Key              string
	KeyStart, KeyEnd int
	Start, End       int
	Value            *Node
	Meta             map[string]any
}

// Get returns the value of key, or nil when the key is absent.
func (n *Node) Get(key string) *Node {
	if n != nil {
		if n.index != nil && n.index.keys != nil {
			return n.index.keys[key]
		}
		for _, e := range n.Entries {
			if e.Key == key {
				return e.Value
			}
		}
	}
	return nil
}

// Entry returns the object entry whose value is child, or nil.
func (n *Node) Entry(child *Node) *Entry {
	if n == nil {
		return nil
	}
	if n.index != nil && n.index.keys != nil {
		if i, ok := n.index.children[child]; ok {
			return n.Entries[i]
		}
		return nil
	}
	for _, e := range n.Entries {
		if e.Value == child {
			return e
		}
	}
	return nil
}

// Index returns the position of child in an array, or -1.
func (n *Node) Index(child *Node) int {
	if n == nil {
		return -1
	}
	if n.index != nil && n.index.keys == nil {
		if i, ok := n.index.children[child]; ok {
			return i
		}
		return -1
	}
	for i, v := range n.Items {
		if v == child {
			return i
		}
	}
	return -1
}

// Interface returns a detached semantic value using maps, slices, and scalars.
func (n *Node) Interface() any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case Object:
		m := make(map[string]any, len(n.Entries))
		for _, e := range n.Entries {
			m[e.Key] = e.Value.Interface()
		}
		return m
	case Array:
		a := make([]any, len(n.Items))
		for i, v := range n.Items {
			a[i] = v.Interface()
		}
		return a
	default:
		return n.Value
	}
}

// Link sets parent pointers and builds lookup indexes for wide containers. Call
// it only while the tree is exclusively owned; subsequent reads do not mutate
// caches. Calling Link again rebuilds indexes after parser-owned tree changes.
func Link(n *Node) {
	if n == nil {
		return
	}
	for _, e := range n.Entries {
		e.Value.Parent = n
		Link(e.Value)
	}
	for _, v := range n.Items {
		v.Parent = n
		Link(v)
	}
	if len(n.Entries) >= indexThreshold {
		if n.index == nil {
			n.index = &nodeIndex{}
		}
		if n.index.keys == nil {
			n.index.keys = make(map[string]*Node, len(n.Entries))
		} else {
			clear(n.index.keys)
		}
		if n.index.children == nil {
			n.index.children = make(map[*Node]int, len(n.Entries))
		} else {
			clear(n.index.children)
		}
		for i, e := range n.Entries {
			// Preserve linear lookup's first-match behavior for custom formats.
			if _, ok := n.index.keys[e.Key]; !ok {
				n.index.keys[e.Key] = e.Value
			}
			if _, ok := n.index.children[e.Value]; !ok {
				n.index.children[e.Value] = i
			}
		}
	} else if len(n.Items) >= indexThreshold {
		if n.index == nil {
			n.index = &nodeIndex{}
		}
		n.index.keys = nil
		if n.index.children == nil {
			n.index.children = make(map[*Node]int, len(n.Items))
		} else {
			clear(n.index.children)
		}
		for i, v := range n.Items {
			if _, ok := n.index.children[v]; !ok {
				n.index.children[v] = i
			}
		}
	} else {
		n.index = nil
	}
}
