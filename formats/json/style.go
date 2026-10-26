package json

import (
	"bytes"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

// insertionStyle describes only whitespace owned by a container. In particular,
// newlines inside its child values do not make a compact container multiline.
type insertionStyle struct {
	multiline       bool
	open, close     string
	before, after   string
	colon, unit, nl string
}

func defaultInsertionStyle() insertionStyle {
	return insertionStyle{colon: ":", unit: "  ", nl: "\n"}
}

func indentation(source []byte, at int) string {
	start := bytes.LastIndexAny(source[:at], "\r\n") + 1
	end := start
	for end < at && (source[end] == ' ' || source[end] == '\t') {
		end++
	}
	return string(source[start:end])
}

func whitespaceLayout(s string, indent, nl string) string {
	if strings.ContainsAny(s, "\r\n") {
		return nl + indent
	}
	return s
}

func containerStyle(source []byte, n *syntax.Node, inherited insertionStyle) insertionStyle {
	s := inherited
	count := len(n.Items)
	if n.Kind == syntax.Object {
		count = len(n.Entries)
	}
	if n.Kind == syntax.Object && len(n.Entries) > 0 {
		e := n.Entries[len(n.Entries)-1]
		s.colon = string(source[e.KeyEnd:e.Value.Start])
	}
	if count == 0 {
		gap := string(source[n.Start+1 : n.End-1])
		if gap == "" {
			return s
		}
		s.multiline = strings.ContainsAny(gap, "\r\n")
		if s.multiline {
			s.nl = newline([]byte(gap))
			if s.colon == ":" {
				s.colon = ": "
			}
		} else {
			s.open, s.close = gap, gap
			s.before, s.after = "", gap
		}
		return s
	}
	first, last := containerSpan(n, 0), containerSpan(n, count-1)
	s.open = string(source[n.Start+1 : first.start])
	s.close = string(source[last.end : n.End-1])
	s.before, s.after = "", s.open
	if count > 1 {
		previous := containerSpan(n, count-2)
		gap := source[previous.end:last.start]
		comma := bytes.IndexByte(gap, ',')
		s.before, s.after = string(gap[:comma]), string(gap[comma+1:])
	}
	s.multiline = strings.ContainsAny(s.open+s.close+s.before+s.after, "\r\n")
	if s.multiline {
		s.nl = newline([]byte(s.open + s.close + s.before + s.after))
		base, child := indentation(source, n.Start), indentation(source, last.start)
		if strings.HasPrefix(child, base) && len(child) > len(base) {
			s.unit = child[len(base):]
		}
		// A single-line opening or separator can coexist with a multiline closing
		// gap. Continue the actual entry separator in that case.
	}
	return s
}

func containerSpan(n *syntax.Node, index int) span {
	if n.Kind == syntax.Object {
		e := n.Entries[index]
		return span{e.Start, e.End}
	}
	child := n.Items[index]
	return span{child.Start, child.End}
}

func surroundingStyle(source []byte, n *syntax.Node) insertionStyle {
	if n == nil {
		return defaultInsertionStyle()
	}
	// Walk from the root so empty containers can inherit the nearest useful
	// convention without scanning unrelated branches of a large document.
	var chain []*syntax.Node
	for p := n; p != nil; p = p.Parent {
		chain = append(chain, p)
	}
	s := defaultInsertionStyle()
	for i := len(chain) - 1; i >= 0; i-- {
		node := chain[i]
		if len(node.Entries) == 0 && len(node.Items) == 0 {
			if sibling := neighboringContainer(node); sibling != nil {
				s = containerStyle(source, sibling, s)
			}
		}
		s = containerStyle(source, node, s)
	}
	return s
}

// Empty containers can borrow a sibling's layout before falling back to their
// ancestors. Prefer the closest preceding container, then the first following
// one. Empty siblings offer no entry convention to infer.
func neighboringContainer(n *syntax.Node) *syntax.Node {
	p := n.Parent
	if p == nil {
		return nil
	}
	var previous *syntax.Node
	seen := false
	count := len(p.Items)
	if p.Kind == syntax.Object {
		count = len(p.Entries)
	}
	for i := 0; i < count; i++ {
		var child *syntax.Node
		if p.Kind == syntax.Object {
			child = p.Entries[i].Value
		} else {
			child = p.Items[i]
		}
		if child == n {
			if previous != nil {
				return previous
			}
			seen = true
			continue
		}
		if child.Kind != n.Kind || (len(child.Entries) == 0 && len(child.Items) == 0) {
			continue
		}
		if seen {
			return child
		}
		previous = child
	}
	return nil
}

// siblingTemplate uses the nearest preceding value of the same structural kind.
// Scalar spelling is never copied: only layout comes from a template.
func siblingTemplate(parent *syntax.Node, kind syntax.Kind) *syntax.Node {
	if parent == nil || (kind != syntax.Array && kind != syntax.Object) {
		return nil
	}
	if parent.Kind == syntax.Object {
		for i := len(parent.Entries) - 1; i >= 0; i-- {
			if n := parent.Entries[i].Value; n.Kind == kind && n.End-n.Start > 2 {
				return n
			}
		}
	} else {
		for i := len(parent.Items) - 1; i >= 0; i-- {
			if n := parent.Items[i]; n.Kind == kind && n.End-n.Start > 2 {
				return n
			}
		}
	}
	return nil
}

func styledValue(compact []byte, value *syntax.Node, source []byte, template *syntax.Node, inherited insertionStyle, base string) []byte {
	if value.Kind != syntax.Object && value.Kind != syntax.Array {
		return compact[value.Start:value.End]
	}
	s := inherited
	if template != nil {
		s = containerStyle(source, template, s)
	}
	open, close := byte('['), byte(']')
	count := len(value.Items)
	if value.Kind == syntax.Object {
		open, close, count = '{', '}', len(value.Entries)
	}
	// Empty newly introduced containers stay compact: they have no entries whose
	// layout needs to be expressed yet.
	if count == 0 {
		return []byte{open, close}
	}
	var b bytes.Buffer
	b.WriteByte(open)
	childIndent := base + s.unit
	if s.multiline {
		b.WriteString(s.nl + childIndent)
	} else {
		b.WriteString(s.open)
	}
	for i := 0; i < count; i++ {
		if i > 0 {
			b.WriteString(whitespaceLayout(s.before, childIndent, s.nl))
			b.WriteByte(',')
			if s.multiline {
				b.WriteString(s.nl + childIndent)
			} else {
				b.WriteString(s.after)
			}
		}
		var child *syntax.Node
		if value.Kind == syntax.Object {
			e := value.Entries[i]
			child = e.Value
			b.WriteString(syntax.Quote(e.Key))
			b.WriteString(s.colon)
		} else {
			child = value.Items[i]
		}
		childTemplate := siblingTemplate(template, child.Kind)
		valueIndent := base
		if s.multiline {
			valueIndent = childIndent
		}
		b.Write(styledValue(compact, child, source, childTemplate, s, valueIndent))
	}
	if s.multiline {
		b.WriteString(s.nl + base)
	} else {
		b.WriteString(s.close)
	}
	b.WriteByte(close)
	return b.Bytes()
}
