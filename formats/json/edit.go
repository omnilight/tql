package json

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/vasileknik76/tql/syntax"
)

func encoded(value any) ([]byte, error) {
	b, _, err := encodedTree(value)
	return b, err
}

func encodedTree(value any) ([]byte, *syntax.Node, error) {
	s, err := syntax.JSON(value)
	if err != nil {
		return nil, nil, err
	}
	// Public callers can construct Number values themselves. Validate their spelling
	// (and non-finite floating-point values) before emitting an invalid document.
	b := []byte(s)
	n, err := (Format{}).Parse(b)
	if err != nil {
		return nil, nil, fmt.Errorf("json: cannot encode value: %w", err)
	}
	return b, n, nil
}

// Replace plans replacement of a selected value.
func (Format) Replace(source []byte, _ *syntax.Node, target *syntax.Node, value any) ([]syntax.Edit, error) {
	if target == nil || target.Start < 0 || target.End > len(source) || target.End < target.Start {
		return nil, fmt.Errorf("json: invalid replacement target")
	}
	b, err := encoded(value)
	if err != nil {
		return nil, err
	}
	return []syntax.Edit{{Start: target.Start, End: target.End, Text: b}}, nil
}

type span struct{ start, end int }

func children(n *syntax.Node) []span {
	var spans []span
	if n.Kind == syntax.Object {
		for _, e := range n.Entries {
			spans = append(spans, span{e.Start, e.End})
		}
	} else {
		for _, child := range n.Items {
			spans = append(spans, span{child.Start, child.End})
		}
	}
	return spans
}

// Delete plans removal of a node and its required syntax.
func (Format) Delete(source []byte, _ *syntax.Node, target *syntax.Node) ([]syntax.Edit, error) {
	if target == nil || target.Parent == nil {
		return nil, fmt.Errorf("json: cannot delete document root")
	}
	parent := target.Parent
	spans := children(parent)
	index := -1
	if parent.Kind == syntax.Object {
		for i, e := range parent.Entries {
			if e.Value == target {
				index = i
				break
			}
		}
	} else if parent.Kind == syntax.Array {
		index = parent.Index(target)
	}
	if index < 0 {
		return nil, fmt.Errorf("json: target is not a container child")
	}
	s := spans[index]
	edits := []syntax.Edit{{Start: s.start, End: s.end}}
	// Remove exactly one adjacent comma; all surrounding whitespace is retained.
	if index+1 < len(spans) {
		at := s.end + bytes.IndexByte(source[s.end:spans[index+1].start], ',')
		edits = append(edits, syntax.Edit{Start: at, End: at + 1})
	} else if index > 0 {
		at := spans[index-1].end + bytes.IndexByte(source[spans[index-1].end:s.start], ',')
		edits = append(edits, syntax.Edit{Start: at, End: at + 1})
	}
	return edits, nil
}

// Insert plans a missing object member or an array append.
func (Format) Insert(source []byte, _ *syntax.Node, parent *syntax.Node, key string, value any) ([]syntax.Edit, error) {
	if parent == nil || parent.Start < 0 || parent.End > len(source) || parent.End <= parent.Start {
		return nil, fmt.Errorf("json: invalid insertion target")
	}
	b, parsed, err := encodedTree(value)
	if err != nil {
		return nil, err
	}
	if parent.Kind != syntax.Object && parent.Kind != syntax.Array {
		return nil, fmt.Errorf("json: can only insert into an object or array")
	}
	style := surroundingStyle(source, parent)
	count := len(parent.Items)
	if parent.Kind == syntax.Object {
		count = len(parent.Entries)
	}
	base := indentation(source, parent.Start)
	entryIndent := base
	if style.multiline {
		entryIndent += style.unit
	}
	if count > 0 && style.multiline {
		entryIndent = indentation(source, containerSpan(parent, count-1).start)
	}
	b = styledValue(b, parsed, source, siblingTemplate(parent, parsed.Kind), style, entryIndent)
	switch parent.Kind {
	case syntax.Object:
		if parent.Get(key) != nil {
			return nil, fmt.Errorf("json: key %q already exists", key)
		}
		b = append([]byte(syntax.Quote(key)+style.colon), b...)
	case syntax.Array:
		if key != "-" && key != strconv.Itoa(len(parent.Items)) {
			return nil, fmt.Errorf("json: array insertion requires '-' or index %d", len(parent.Items))
		}
	default:
		return nil, fmt.Errorf("json: can only insert into an object or array")
	}
	if count == 0 {
		gap := source[parent.Start+1 : parent.End-1]
		prefix, suffix := style.open, ""
		if style.multiline {
			prefix = style.nl + entryIndent
			if !bytes.ContainsAny(gap, "\r\n") {
				suffix = style.nl + base
			}
		} else if len(gap) == 0 {
			suffix = style.close
		}
		text := append([]byte(prefix), b...)
		text = append(text, suffix...)
		return []syntax.Edit{{Start: parent.Start + 1, End: parent.Start + 1, Text: text}}, nil
	}
	separator := whitespaceLayout(style.before, entryIndent, style.nl) + "," + whitespaceLayout(style.after, entryIndent, style.nl)
	at := containerSpan(parent, count-1).end
	return []syntax.Edit{{Start: at, End: at, Text: append([]byte(separator), b...)}}, nil
}

func newline(b []byte) string {
	if bytes.Contains(b, []byte("\r\n")) {
		return "\r\n"
	}
	if bytes.IndexByte(b, '\r') >= 0 && bytes.IndexByte(b, '\n') < 0 {
		return "\r"
	}
	return "\n"
}
