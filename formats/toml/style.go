package toml

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

// insertionStyle describes only trivia and quoting. Source comments and values
// are never used as generated text.
type insertionStyle struct {
	before, after, separator, commaBefore, arrayPadding, objectPadding string
	indent, unit, nl                                                   string
	keyQuote, stringQuote                                              byte
	equalsColumn                                                       int
}

func defaultInsertionStyle(src []byte) insertionStyle {
	return insertionStyle{before: " ", after: " ", separator: " ", objectPadding: " ", unit: "  ", nl: newlineStyle(src), equalsColumn: -1}
}

func horizontal(s string) bool { return strings.Trim(s, " \t") == "" }

func sourceIndent(src []byte, pos int) string {
	start := pos
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	gap := string(src[start:pos])
	if horizontal(gap) {
		return gap
	}
	return ""
}

// sectionEntries visits dotted assignments, but does not cross table headers or
// descend into concrete values belonging to another assignment.
func visitSectionEntries(n *syntax.Node, visit func(*syntax.Entry)) {
	for _, e := range n.Entries {
		if e.Start >= 0 {
			visit(e)
		} else if e.Value.Kind == syntax.Object && e.Value.Meta["explicit"] != true {
			visitSectionEntries(e.Value, visit)
		}
	}
}

func sectionEntries(n *syntax.Node) []*syntax.Entry {
	entries := make([]*syntax.Entry, 0, len(n.Entries))
	visitSectionEntries(n, func(e *syntax.Entry) { entries = append(entries, e) })
	if !sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].Start < entries[j].Start }) {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Start < entries[j].Start })
	}
	return entries
}

func entryStyle(src []byte, e *syntax.Entry, s insertionStyle) insertionStyle {
	if e.Start < 0 || e.KeyEnd < 0 || e.Value.Start < 0 || e.Value.Start > len(src) {
		return s
	}
	end := e.KeyEnd
	for end > e.KeyStart && (src[end-1] == ' ' || src[end-1] == '\t') {
		end--
	}
	if e.KeyEnd < len(src) && src[e.KeyEnd] == '=' {
		s.before = string(src[end:e.KeyEnd])
		s.after = string(src[e.KeyEnd+1 : e.Value.Start])
	}
	s.indent = sourceIndent(src, e.Start)
	s.equalsColumn = -1
	s.keyQuote = 0
	if e.KeyStart < len(src) && (src[e.KeyStart] == '\'' || src[e.KeyStart] == '"') {
		s.keyQuote = src[e.KeyStart]
	}
	return s
}

func localInsertionStyle(src []byte, n *syntax.Node) insertionStyle {
	var chain []*syntax.Node
	for cur := n; cur != nil; cur = cur.Parent {
		chain = append(chain, cur)
	}
	s := defaultInsertionStyle(src)
	for i := len(chain) - 1; i >= 0; i-- {
		cur := chain[i]
		s = containerWhitespace(src, cur, s)
		var previous, last, quoted *syntax.Entry
		visitSectionEntries(cur, func(e *syntax.Entry) {
			if last == nil || e.Start > last.Start {
				previous = last
				last = e
			} else if previous == nil || e.Start > previous.Start {
				previous = e
			}
			if e.Value.Kind == syntax.String && (quoted == nil || e.Start > quoted.Start) {
				quoted = e
			}
		})
		if last != nil {
			s = entryStyle(src, last, s)
			if cur.Start < 0 && previous != nil {
				s.equalsColumn = alignedEqualsColumn(src, []*syntax.Entry{previous, last})
			}
		}
		if quoted != nil {
			s.stringQuote = src[quoted.Value.Start]
		}

		if cur.Kind == syntax.Array && len(cur.Items) > 0 {
			for j := len(cur.Items) - 1; j >= 0; j-- {
				v := cur.Items[j]
				if v.Kind == syntax.String && v.Start >= 0 {
					s.stringQuote = src[v.Start]
					break
				}
			}
		}
	}
	return s
}

func literalString(value string) bool {
	for _, r := range value {
		if r == '\'' || r == '\n' || r == '\r' || r < 0x20 && r != '\t' || r == 0x7f {
			return false
		}
	}
	return true
}

func styledString(value string, q byte) string {
	if q == '\'' && literalString(value) {
		return "'" + value + "'"
	}
	return quoteString(value)
}

func styledKey(key string, s insertionStyle) string {
	if s.keyQuote == '\'' {
		return styledString(key, '\'')
	}
	if s.keyQuote == '"' {
		return quoteString(key)
	}
	return quoteKey(key)
}

func styledPath(keys []string, s insertionStyle) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = styledKey(k, s)
	}
	return strings.Join(parts, ".")
}

func valueKind(value any) syntax.Kind {
	switch value.(type) {
	case map[string]any:
		return syntax.Object
	case []any:
		return syntax.Array
	case string:
		return syntax.String
	}
	return ""
}

func insertionTemplate(n *syntax.Node, kind syntax.Kind) *syntax.Node {
	if kind == "" {
		return nil
	}
	for cur := n; cur != nil; cur = cur.Parent {
		for i := len(cur.Items) - 1; i >= 0; i-- {
			v := cur.Items[i]
			if v != n && v.Kind == kind && v.Start >= 0 {
				return v
			}
		}
		var result *syntax.Node
		visitSectionEntries(cur, func(e *syntax.Entry) {
			v := e.Value
			if v != n && v.Kind == kind && v.Start >= 0 && (result == nil || v.Start > result.Start) {
				result = v
			}
		})
		if result != nil {
			return result
		}
	}
	return nil
}

func containerWhitespace(src []byte, n *syntax.Node, s insertionStyle) insertionStyle {
	if n == nil || n.Start < 0 || n.End > len(src) {
		return s
	}
	if n.Kind != syntax.Array && n.Kind != syntax.Object {
		return s
	}
	first := n.End - 1
	if n.Kind == syntax.Array && len(n.Items) > 0 {
		first = n.Items[0].Start
	}
	if n.Kind == syntax.Object {
		entries := sectionEntries(n)
		if len(entries) > 0 {
			first = entries[0].Start
			s = entryStyle(src, entries[len(entries)-1], s)
		}
	}
	opening := string(src[n.Start+1 : first])
	if horizontal(opening) {
		if n.Kind == syntax.Array {
			s.arrayPadding = opening
		} else {
			s.objectPadding = opening
		}
	}
	if n.Kind == syntax.Array && len(n.Items) > 1 {
		a, b := n.Items[len(n.Items)-2], n.Items[len(n.Items)-1]
		c := comma(src, a.End, b.Start)
		if c >= 0 && horizontal(string(src[c+1:b.Start])) {
			s.separator = string(src[c+1 : b.Start])
			if horizontal(string(src[a.End:c])) {
				s.commaBefore = string(src[a.End:c])
			}
		}
	} else if n.Kind == syntax.Object {
		entries := sectionEntries(n)
		if len(entries) > 1 {
			a, b := entries[len(entries)-2], entries[len(entries)-1]
			c := comma(src, a.End, b.Start)
			if c >= 0 && horizontal(string(src[c+1:b.Start])) {
				s.separator = string(src[c+1 : b.Start])
				if horizontal(string(src[a.End:c])) {
					s.commaBefore = string(src[a.End:c])
				}
			}
		} else if horizontal(opening) {
			s.separator = opening
		}
	} else if horizontal(opening) {
		s.separator = opening
	}
	if n.Kind == syntax.Array && len(n.Items) > 0 {
		last := n.Items[len(n.Items)-1]
		if c := comma(src, last.End, n.End-1); c >= 0 && horizontal(string(src[last.End:c])) {
			s.commaBefore = string(src[last.End:c])
		}
	}
	return s
}

func renderInsertion(src []byte, value any, template *syntax.Node, s insertionStyle, depth int) (string, error) {
	if depth > 256 {
		return "", fmt.Errorf("TOML value nesting exceeds 256 levels")
	}
	if template != nil {
		s = containerWhitespace(src, template, s)
	}
	switch v := value.(type) {
	case string:
		q := s.stringQuote
		if template != nil && template.Kind == syntax.String && template.Start >= 0 {
			q = src[template.Start]
		}
		return styledString(v, q), nil
	case []any:
		parts := make([]string, len(v))
		var exemplar *syntax.Node
		if template != nil && template.Kind == syntax.Array && len(template.Items) > 0 {
			exemplar = template.Items[len(template.Items)-1]
		}
		multi := template != nil && template.Start >= 0 && strings.Contains(string(src[template.Start:template.End]), "\n")
		itemIndent := s.indent + s.unit
		if multi && len(template.Items) > 0 {
			base := sourceIndent(src, template.Start)
			if gap := sourceIndent(src, template.Items[len(template.Items)-1].Start); strings.HasPrefix(gap, base) {
				itemIndent = s.indent + strings.TrimPrefix(gap, base)
			}
		}
		for i, x := range v {
			child := exemplar
			if child != nil && child.Kind != valueKind(x) {
				child = nil
			}
			var err error
			childStyle := s
			childStyle.indent = itemIndent
			parts[i], err = renderInsertion(src, x, child, childStyle, depth+1)
			if err != nil {
				return "", err
			}
		}
		if len(parts) == 0 {
			return "[" + s.arrayPadding + "]", nil
		}
		if multi {
			trailing := false
			last := template.Items
			if len(last) > 0 {
				trailing = comma(src, last[len(last)-1].End, template.End-1) >= 0
			}
			body := strings.Join(parts, s.commaBefore+","+s.nl+itemIndent)
			if trailing {
				body += s.commaBefore + ","
			}
			return "[" + s.nl + itemIndent + body + s.nl + s.indent + "]", nil
		}
		return "[" + s.arrayPadding + strings.Join(parts, s.commaBefore+","+s.separator) + s.arrayPadding + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		templates := make(map[syntax.Kind]*syntax.Node, 3)
		for _, k := range keys {
			var child *syntax.Node
			if template != nil && template.Kind == syntax.Object {
				child = template.Get(k)
				if child == nil {
					kind := valueKind(v[k])
					var found bool
					child, found = templates[kind]
					if !found {
						child = insertionTemplate(template, kind)
						templates[kind] = child
					}
				}
			}
			text, err := renderInsertion(src, v[k], child, s, depth+1)
			if err != nil {
				return "", err
			}
			parts = append(parts, styledKey(k, s)+s.before+"="+s.after+text)
		}
		if len(parts) == 0 {
			return "{" + s.objectPadding + "}", nil
		}
		return "{" + s.objectPadding + strings.Join(parts, s.commaBefore+","+s.separator) + s.objectPadding + "}", nil
	default:
		return render(value)
	}
}

func insertArrayStyled(src []byte, parent *syntax.Node, value any) ([]syntax.Edit, error) {
	s := localInsertionStyle(src, parent)
	template := insertionTemplate(parent, valueKind(value))
	open, close := parent.Start+1, parent.End-1
	multiline := strings.Contains(string(src[open:close]), "\n")
	if !multiline {
		text, err := renderInsertion(src, value, template, s, 0)
		if err != nil {
			return nil, err
		}
		if len(parent.Items) == 0 {
			gap := string(src[open:close])
			return []syntax.Edit{edit(open, open, gap+text)}, nil
		}
		last := parent.Items[len(parent.Items)-1]
		if c := comma(src, last.End, close); c >= 0 {
			return []syntax.Edit{edit(c+1, c+1, s.separator+text+s.commaBefore+",")}, nil
		}
		return []syntax.Edit{edit(last.End, last.End, s.commaBefore+","+s.separator+text)}, nil
	}
	lineStart := close
	for lineStart > open && src[lineStart-1] != '\n' {
		lineStart--
	}
	closingLine := horizontal(string(src[lineStart:close]))
	indent := string(src[lineStart:close]) + s.unit
	if !closingLine {
		indent = s.indent + s.unit
	}
	if len(parent.Items) > 0 {
		last := parent.Items[len(parent.Items)-1]
		start := last.Start
		for start > 0 && src[start-1] != '\n' {
			start--
		}
		if horizontal(string(src[start:last.Start])) {
			indent = string(src[start:last.Start])
		}
	}
	s.indent = indent
	text, err := renderInsertion(src, value, template, s, 0)
	if err != nil {
		return nil, err
	}
	if len(parent.Items) == 0 {
		if closingLine {
			return []syntax.Edit{edit(lineStart, lineStart, indent+text+s.nl)}, nil
		}
		return []syntax.Edit{edit(close, close, s.nl+indent+text+s.nl+s.indent)}, nil
	}
	last := parent.Items[len(parent.Items)-1]
	c := comma(src, last.End, close)
	trailing := c >= 0
	suffix := ""
	if trailing {
		suffix = s.commaBefore + ","
	}
	if closingLine && lineStart > last.End {
		edits := []syntax.Edit{edit(lineStart, lineStart, indent+text+suffix+s.nl)}
		if !trailing {
			edits = append(edits, edit(last.End, last.End, s.commaBefore+","))
		}
		return edits, nil
	}
	if trailing {
		return []syntax.Edit{edit(c+1, c+1, s.nl+indent+text+s.commaBefore+",")}, nil
	}
	return []syntax.Edit{edit(last.End, last.End, s.commaBefore+","+s.nl+indent+text)}, nil
}

func insertInlineAssignment(src []byte, owner *syntax.Node, assignment string, s insertionStyle) []syntax.Edit {
	open, close := owner.Start+1, owner.End-1
	entries := inlineEntries(owner)
	if !strings.Contains(string(src[open:close]), "\n") {
		if len(entries) == 0 {
			gap := string(src[open:close])
			return []syntax.Edit{edit(open, open, gap+assignment)}
		}
		last := entries[len(entries)-1]
		if c := comma(src, last.End, close); c >= 0 {
			return []syntax.Edit{edit(c+1, c+1, s.separator+assignment+s.commaBefore+",")}
		}
		return []syntax.Edit{edit(last.End, last.End, s.commaBefore+","+s.separator+assignment)}
	}
	lineStart := close
	for lineStart > open && src[lineStart-1] != '\n' {
		lineStart--
	}
	closingLine := horizontal(string(src[lineStart:close]))
	indent := s.indent + s.unit
	if closingLine {
		indent = string(src[lineStart:close]) + s.unit
	}
	if len(entries) > 0 {
		last := entries[len(entries)-1]
		start := last.Start
		for start > 0 && src[start-1] != '\n' {
			start--
		}
		if horizontal(string(src[start:last.Start])) {
			indent = string(src[start:last.Start])
		}
	}
	if len(entries) == 0 {
		if closingLine {
			return []syntax.Edit{edit(lineStart, lineStart, indent+assignment+s.nl)}
		}
		return []syntax.Edit{edit(close, close, s.nl+indent+assignment+s.nl+s.indent)}
	}
	last := entries[len(entries)-1]
	c := comma(src, last.End, close)
	suffix := ""
	if c >= 0 {
		suffix = s.commaBefore + ","
	}
	if closingLine && lineStart > last.End {
		edits := []syntax.Edit{edit(lineStart, lineStart, indent+assignment+suffix+s.nl)}
		if c < 0 {
			edits = append(edits, edit(last.End, last.End, s.commaBefore+","))
		}
		return edits
	}
	if c >= 0 {
		return []syntax.Edit{edit(c+1, c+1, s.nl+indent+assignment+s.commaBefore+",")}
	}
	return []syntax.Edit{edit(last.End, last.End, s.commaBefore+","+s.nl+indent+assignment)}
}

func renderTableInsertion(src []byte, m map[string]any, template *syntax.Node, s insertionStyle) (string, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	templates := make(map[syntax.Kind]*syntax.Node, 3)
	for _, k := range keys {
		var child *syntax.Node
		if template != nil {
			child = template.Get(k)
			if child == nil {
				kind := valueKind(m[k])
				var found bool
				child, found = templates[kind]
				if !found {
					child = insertionTemplate(template, kind)
					templates[kind] = child
				}
			}
		}
		text, err := renderInsertion(src, m[k], child, s, 0)
		if err != nil {
			return "", err
		}
		b.WriteString(s.indent + styledAssignment(styledKey(k, s), text, s) + s.nl)
	}
	return b.String(), nil
}

// Treat equals signs as aligned only when at least two differently sized keys
// demonstrate the same column. Fixed-width spacing alone is not evidence.
func alignedEqualsColumn(src []byte, entries []*syntax.Entry) int {
	if len(entries) < 2 {
		return -1
	}
	a, b := entries[len(entries)-2], entries[len(entries)-1]
	columns := [2]int{}
	lengths := [2]int{}
	for i, e := range []*syntax.Entry{a, b} {
		start := e.Start
		for start > 0 && src[start-1] != '\n' {
			start--
		}
		if !horizontal(string(src[start:e.Start])) || strings.Contains(string(src[start:e.KeyEnd]), "\t") {
			return -1
		}
		columns[i] = e.KeyEnd - start
		end := e.KeyEnd
		for end > e.KeyStart && src[end-1] == ' ' {
			end--
		}
		lengths[i] = end - e.KeyStart
	}
	if columns[0] == columns[1] && lengths[0] != lengths[1] {
		return columns[0]
	}
	return -1
}

func styledAssignment(key, value string, s insertionStyle) string {
	before := s.before
	if s.equalsColumn >= 0 {
		width := s.equalsColumn - len(s.indent) - len(key)
		if width > 0 {
			before = strings.Repeat(" ", width)
		} else {
			before = " "
		}
	}
	return key + before + "=" + s.after + value
}
