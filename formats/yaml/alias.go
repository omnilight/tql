package yaml

import (
	"fmt"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

type anchorInfo struct {
	name       string
	start, end int
}

func property(src []byte, start, end int) (anchorInfo, int, error) {
	pos := start + 1
	for pos < end && !strings.ContainsRune(" \t\r\n[]{},", rune(src[pos])) {
		pos++
	}
	if pos == start+1 {
		return anchorInfo{}, pos, errAt(start, "empty anchor or alias name")
	}
	name := string(src[start+1 : pos])
	return anchorInfo{name, start, pos}, pos, nil
}
func (p *parser) bind(n *syntax.Node, a anchorInfo) error {
	if _, alias := n.Meta["aliasName"]; alias {
		return errAt(a.start, "alias nodes cannot have anchor properties")
	}
	if _, exists := n.Meta["anchorName"]; exists {
		return errAt(a.start, "duplicate anchor properties")
	}
	if p.anchors == nil {
		p.anchors = map[string]*syntax.Node{}
	}
	if _, ok := p.anchors[a.name]; ok {
		return errAt(a.start, "duplicate anchors are unsupported")
	}
	n.Meta = meta(n.Meta)
	n.Meta["anchorName"] = a.name
	n.Meta["anchorStart"] = a.start
	n.Meta["anchorEnd"] = a.end
	p.anchors[a.name] = n
	return nil
}
func (p *parser) alias(a anchorInfo) (*syntax.Node, error) {
	source := p.anchors[a.name]
	if source == nil {
		return nil, errAt(a.start, "unknown, forward or cyclic alias "+a.name)
	}
	var clone func(*syntax.Node, int) (*syntax.Node, error)
	clone = func(n *syntax.Node, depth int) (*syntax.Node, error) {
		p.aliasNodes++
		if p.aliasNodes > 100000 || depth > 256 {
			return nil, errAt(a.start, "alias expansion limit exceeded")
		}
		out := &syntax.Node{Kind: n.Kind, Value: n.Value, Start: a.start, End: a.end, Style: "alias"}
		for _, entry := range n.Entries {
			v, e := clone(entry.Value, depth+1)
			if e != nil {
				return nil, e
			}
			out.Entries = append(out.Entries, &syntax.Entry{Key: entry.Key, KeyStart: a.start, KeyEnd: a.end, Start: a.start, End: a.end, Value: v})
		}
		for _, item := range n.Items {
			v, e := clone(item, depth+1)
			if e != nil {
				return nil, e
			}
			out.Items = append(out.Items, v)
		}
		return out, nil
	}
	n, e := clone(source, 0)
	if e != nil {
		return nil, e
	}
	n.Meta = map[string]any{"aliasName": a.name}
	return n, nil
}
func pending(n *syntax.Node) (anchorInfo, bool) {
	a, ok := n.Meta["pendingAnchor"].(anchorInfo)
	return a, ok
}

type aliasStep struct {
	key    string
	index  int
	object bool
}

func aliasPath(n *syntax.Node) (*syntax.Node, []aliasStep) {
	var rev []aliasStep
	for current := n; current != nil; current = current.Parent {
		if _, ok := current.Meta["aliasName"]; ok {
			path := make([]aliasStep, len(rev))
			for i := range rev {
				path[len(rev)-1-i] = rev[i]
			}
			return current, path
		}
		parent := current.Parent
		if parent == nil {
			break
		}
		if parent.Kind == syntax.Object {
			e := parent.Entry(current)
			if e == nil {
				return nil, nil
			}
			rev = append(rev, aliasStep{key: e.Key, object: true})
		} else {
			rev = append(rev, aliasStep{index: parent.Index(current)})
		}
	}
	return nil, nil
}
func aliasEdit(n *syntax.Node, op, key string, value any) ([]syntax.Edit, bool, error) {
	root, path := aliasPath(n)
	if root == nil || op == "delete" && len(path) == 0 {
		return nil, false, nil
	}
	tree := root.Interface()
	var mutate func(any, []aliasStep) (any, error)
	mutate = func(current any, path []aliasStep) (any, error) {
		if len(path) == 0 {
			switch op {
			case "replace":
				return value, nil
			case "insert":
				switch x := current.(type) {
				case map[string]any:
					if _, exists := x[key]; exists {
						return nil, fmt.Errorf("yaml: mapping key already exists")
					}
					x[key] = value
					return x, nil
				case []any:
					if key != "" && key != "-" && key != fmt.Sprint(len(x)) {
						return nil, fmt.Errorf("yaml: array insertion only supports append")
					}
					return append(x, value), nil
				}
				return nil, fmt.Errorf("yaml: cannot insert into scalar")
			}
			return nil, fmt.Errorf("yaml: invalid alias operation")
		}
		s := path[0]
		if s.object {
			m := current.(map[string]any)
			if len(path) == 1 && op == "delete" {
				delete(m, s.key)
				return m, nil
			}
			v, e := mutate(m[s.key], path[1:])
			if e != nil {
				return nil, e
			}
			m[s.key] = v
			return m, nil
		}
		a := current.([]any)
		if len(path) == 1 && op == "delete" {
			return append(a[:s.index], a[s.index+1:]...), nil
		}
		v, e := mutate(a[s.index], path[1:])
		if e != nil {
			return nil, e
		}
		a[s.index] = v
		return a, nil
	}
	changed, e := mutate(tree, path)
	if e != nil {
		return nil, true, e
	}
	text, e := encode(changed)
	if e != nil {
		return nil, true, e
	}
	return []syntax.Edit{{Start: root.Start, End: root.End, Text: []byte(text)}}, true, nil
}
