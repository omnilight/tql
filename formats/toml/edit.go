package toml

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

func render(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", fmt.Errorf("TOML has no null value")
	case syntax.Temporal:
		if !temporalRE.MatchString(string(v)) || !validTime(string(v)) {
			return "", fmt.Errorf("invalid TOML date/time")
		}
		return string(v), nil
	case string:
		return quoteString(v), nil
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		if math.IsNaN(v) {
			return "nan", nil
		}
		if math.IsInf(v, 1) {
			return "inf", nil
		}
		if math.IsInf(v, -1) {
			return "-inf", nil
		}
		s := strconv.FormatFloat(v, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s, nil
	case syntax.Number:
		p := &parser{src: []byte(v)}
		n, e := p.atom()
		if e != nil || p.pos != len(p.src) || n.Kind != syntax.NumberKind {
			return "", fmt.Errorf("invalid TOML number %q", v)
		}
		return string(v), nil
	case []any:
		parts := make([]string, len(v))
		for i, x := range v {
			s, e := render(x)
			if e != nil {
				return "", e
			}
			parts[i] = s
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			s, e := render(v[k])
			if e != nil {
				return "", e
			}
			parts = append(parts, quoteKey(k)+" = "+s)
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	default:
		return "", fmt.Errorf("unsupported TOML value %T", value)
	}
}

func quoteString(s string) string { return strings.ReplaceAll(syntax.Quote(s), "\x7f", `\u007f`) }

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func quoteKey(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return quoteString(k)
}
func edit(start, end int, s string) syntax.Edit {
	return syntax.Edit{Start: start, End: end, Text: []byte(s)}
}

func parentEntry(n *syntax.Node) (*syntax.Entry, error) {
	if n.Parent == nil {
		return nil, fmt.Errorf("root has no entry")
	}
	e := n.Parent.Entry(n)
	if e == nil {
		return nil, fmt.Errorf("value is not an object member")
	}
	return e, nil
}
func newlineStyle(src []byte) string {
	if bytes.Contains(src, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// context finds a concrete inline table or the nearest explicit table section.
func context(n *syntax.Node) (*syntax.Node, []string, error) {
	var reverse []string
	for {
		if n.Kind != syntax.Object {
			return nil, nil, fmt.Errorf("invalid TOML insertion parent")
		}
		if n.Style == "inline" || n.Parent == nil || n.Meta["explicit"] == true {
			for i, j := 0, len(reverse)-1; i < j; i, j = i+1, j-1 {
				reverse[i], reverse[j] = reverse[j], reverse[i]
			}
			return n, reverse, nil
		}
		e, err := parentEntry(n)
		if err != nil {
			return nil, nil, err
		}
		reverse = append(reverse, e.Key)
		n = n.Parent
	}
}
func pathText(keys []string) string {
	a := make([]string, len(keys))
	for i, k := range keys {
		a[i] = quoteKey(k)
	}
	return strings.Join(a, ".")
}
func allHeaders(n *syntax.Node, positions *[]int) {
	if s, ok := n.Meta["headerStart"].(int); ok {
		*positions = append(*positions, s)
	}
	for _, e := range n.Entries {
		allHeaders(e.Value, positions)
	}
	for _, v := range n.Items {
		allHeaders(v, positions)
	}
}
func insertion(src []byte, root, parent *syntax.Node, key string, value any) (syntax.Edit, error) {
	owner, path, err := context(parent)
	if err != nil {
		return syntax.Edit{}, err
	}
	style := localInsertionStyle(src, parent)
	template := insertionTemplate(parent, valueKind(value))
	text, err := renderInsertion(src, value, template, style, 0, owner.Style == "inline")
	if err != nil {
		return syntax.Edit{}, err
	}
	path = append(path, key)
	assignment := styledAssignment(styledPath(path, style), text, style)
	if owner.Style == "inline" {
		if len(owner.Entries) == 0 {
			gap := string(src[owner.Start+1 : owner.End-1])
			return edit(owner.Start+1, owner.Start+1, gap+assignment), nil
		}
		last := maxSyntaxEnd(owner)
		return edit(last, last, style.commaBefore+","+style.separator+assignment), nil
	}
	start := 0
	if x, ok := owner.Meta["headerEnd"].(int); ok {
		start = x
	}
	pos := len(src)
	var hs []int
	allHeaders(root, &hs)
	for _, h := range hs {
		if h >= start && h < pos {
			pos = h
		}
	}
	prefix := ""
	if pos > 0 && src[pos-1] != '\n' {
		prefix = newlineStyle(src)
	}
	suffix := newlineStyle(src)
	if pos == len(src) && pos > 0 && src[pos-1] != '\n' {
		suffix = ""
	}
	return edit(pos, pos, prefix+style.indent+assignment+suffix), nil
}
func maxSyntaxEnd(n *syntax.Node) int {
	max := n.Start + 1
	for _, e := range n.Entries {
		if e.End > max {
			max = e.End
		}
		if e.Start < 0 {
			if x := maxSyntaxEnd(e.Value); x > max {
				max = x
			}
		}
	}
	return max
}

// Insert plans a missing object member or an array append.
func (Format) Insert(src []byte, root, parent *syntax.Node, key string, value any) ([]syntax.Edit, error) {
	if root == nil || parent == nil {
		return nil, fmt.Errorf("toml: invalid insertion target")
	}
	if parent.Kind == syntax.Array {
		if key != "-" && key != strconv.Itoa(len(parent.Items)) {
			return nil, fmt.Errorf("toml: array insertion requires '-' or index %d", len(parent.Items))
		}
		if parent.Style == "tables" {
			m, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("array tables require object items")
			}
			keys, err := absolutePath(parent)
			if err != nil {
				return nil, err
			}
			var template *syntax.Node
			if len(parent.Items) > 0 {
				template = parent.Items[len(parent.Items)-1]
			}
			style := localInsertionStyle(src, parent)
			if template != nil {
				style = localInsertionStyle(src, template)
			}
			body, err := renderTableInsertion(src, m, template, style)
			if err != nil {
				return nil, err
			}
			pos := len(src)
			for ancestor := parent.Parent; ancestor != nil; ancestor = ancestor.Parent {
				if ancestor.Parent != nil && ancestor.Parent.Style == "tables" {
					siblings := ancestor.Parent.Items
					index := ancestor.Parent.Index(ancestor)
					if index+1 < len(siblings) {
						if next, ok := siblings[index+1].Meta["headerStart"].(int); ok && next < pos {
							pos = next
						}
					}
				}
			}
			prefix := ""
			if pos > 0 && src[pos-1] != '\n' {
				prefix = newlineStyle(src)
			}
			header := "[[" + pathText(keys) + "]]"
			if template != nil {
				if start, ok := template.Meta["headerStart"].(int); ok {
					end := template.Meta["headerEnd"].(int)
					header = sourceIndent(src, start) + string(src[start:end])
				}
			}
			addition := prefix + header + style.nl + body
			if pos == len(src) && pos > 0 && src[pos-1] != '\n' {
				addition = strings.TrimSuffix(addition, style.nl)
			}
			return []syntax.Edit{edit(pos, pos, addition)}, nil
		}
		return insertArrayStyled(src, parent, value)
	}
	if parent.Kind != syntax.Object {
		return nil, fmt.Errorf("TOML insertion requires object or array")
	}
	if parent.Get(key) != nil {
		return nil, fmt.Errorf("key %q already exists", key)
	}
	e, err := insertion(src, root, parent, key, value)
	if err != nil {
		return nil, err
	}
	return []syntax.Edit{e}, nil
}
func absolutePath(n *syntax.Node) ([]string, error) {
	var keys []string
	for n.Parent != nil {
		if n.Parent.Kind == syntax.Array {
			n = n.Parent
			continue
		}
		e, err := parentEntry(n)
		if err != nil {
			return nil, err
		}
		keys = append(keys, e.Key)
		n = n.Parent
	}
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	return keys, nil
}
func renderDocument(m map[string]any) (string, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		s, e := render(m[k])
		if e != nil {
			return "", e
		}
		b.WriteString(quoteKey(k) + " = " + s + "\n")
	}
	return b.String(), nil
}
func ownedEdits(n *syntax.Node) []syntax.Edit {
	var edits []syntax.Edit
	if s, ok := n.Meta["headerStart"].(int); ok {
		edits = append(edits, edit(s, n.Meta["headerEnd"].(int), ""))
	}
	for _, e := range n.Entries {
		if e.Start >= 0 {
			edits = append(edits, edit(e.Start, e.End, ""))
		} else {
			edits = append(edits, ownedEdits(e.Value)...)
		}
	}
	for _, v := range n.Items {
		edits = append(edits, ownedEdits(v)...)
	}
	return edits
}

// Replace plans replacement of a selected value.
func (f Format) Replace(src []byte, root, target *syntax.Node, value any) ([]syntax.Edit, error) {
	if root == nil || target == nil || target.Start >= 0 && (target.End < target.Start || target.End > len(src)) {
		return nil, fmt.Errorf("toml: invalid replacement target")
	}
	if target.Start >= 0 {
		s, err := render(value)
		if err != nil {
			return nil, err
		}
		return []syntax.Edit{edit(target.Start, target.End, s)}, nil
	}
	if target.Parent == nil {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("TOML root must remain an object")
		}
		s, err := renderDocument(m)
		if err != nil {
			return nil, err
		}
		edits := ownedEdits(target)
		return append([]syntax.Edit{edit(0, 0, s)}, edits...), nil
	}
	if target.Parent.Kind == syntax.Array {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("an array-table item must remain an object; replace its whole array to change its type")
		}
		body, err := renderDocument(m)
		if err != nil {
			return nil, err
		}
		body = strings.ReplaceAll(body, "\n", newlineStyle(src))
		start := target.Meta["headerStart"].(int)
		end := target.Meta["headerEnd"].(int)
		// Keep the original header and its trailing comment in place.
		pos := end
		for pos < len(src) && src[pos] != '\n' {
			pos++
		}
		if pos < len(src) {
			pos++
		} else {
			body = newlineStyle(src) + body
		}
		edits := ownedEdits(target)
		kept := edits[:0]
		for _, e := range edits {
			if e.Start != start || e.End != end {
				kept = append(kept, e)
			}
		}
		return append([]syntax.Edit{edit(pos, pos, body)}, kept...), nil
	}
	if owner := enclosingInline(target.Parent); owner != nil {
		_, path, err := context(target.Parent)
		if err != nil {
			return nil, err
		}
		member, err := parentEntry(target)
		if err != nil {
			return nil, err
		}
		path = append(path, member.Key)
		s, err := render(value)
		if err != nil {
			return nil, err
		}
		entries := inlineEntries(owner)
		var edits []syntax.Edit
		first := true
		for i, e := range entries {
			if !descendant(e.Value, target) {
				continue
			}
			if first {
				edits = append(edits, edit(e.Start, e.End, pathText(path)+" = "+s))
				first = false
			} else {
				edits = append(edits, edit(e.Start, e.End, ""))
				if i > 0 {
					c := comma(src, entries[i-1].End, e.Start)
					if c >= 0 {
						edits = append(edits, edit(c, c+1, ""))
					}
				}
			}
		}
		return edits, nil
	}
	e, err := parentEntry(target)
	if err != nil {
		return nil, err
	}
	ins, err := insertion(src, root, target.Parent, e.Key, value)
	if err != nil {
		return nil, err
	}
	return append([]syntax.Edit{ins}, ownedEdits(target)...), nil
}
func comma(src []byte, start, end int) int {
	comment := false
	for i := start; i < end; i++ {
		if src[i] == '\n' {
			comment = false
		}
		if src[i] == '#' {
			comment = true
		}
		if src[i] == ',' && !comment {
			return i
		}
	}
	return -1
}
func deleteDelimited(src []byte, container *syntax.Node, start, end, index, count int, prevEnd, nextStart int) ([]syntax.Edit, error) {
	edits := []syntax.Edit{edit(start, end, "")}
	c := comma(src, end, nextStart)
	if c >= 0 {
		edits = append(edits, edit(c, c+1, ""))
	} else if index > 0 {
		c = comma(src, prevEnd, start)
		if c < 0 {
			return nil, fmt.Errorf("missing separator")
		}
		edits = append(edits, edit(c, c+1, ""))
	}
	return edits, nil
}
func enclosingInline(n *syntax.Node) *syntax.Node {
	for n != nil {
		if n.Style == "inline" {
			return n
		}
		n = n.Parent
	}
	return nil
}
func inlineEntries(n *syntax.Node) []*syntax.Entry {
	var out []*syntax.Entry
	for _, e := range n.Entries {
		if e.Start >= 0 {
			out = append(out, e)
		} else {
			out = append(out, inlineEntries(e.Value)...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}
func descendant(n, ancestor *syntax.Node) bool {
	for n != nil {
		if n == ancestor {
			return true
		}
		n = n.Parent
	}
	return false
}

// Delete plans removal of a node and its required syntax.
func (f Format) Delete(src []byte, root, target *syntax.Node) ([]syntax.Edit, error) {
	if root == nil || target == nil || target.Start >= 0 && (target.End < target.Start || target.End > len(src)) {
		return nil, fmt.Errorf("toml: invalid deletion target")
	}
	if target.Parent == nil {
		return nil, fmt.Errorf("cannot delete TOML root")
	}
	parent := target.Parent
	// A dotted assignment may be the only source spelling of its parent table.
	// Preserve the now-empty parent rather than silently deleting that table too.
	if parent.Kind == syntax.Object && len(parent.Entries) == 1 && parent.Start < 0 && parent.Meta["explicit"] != true && parent.Parent != nil {
		return f.Replace(src, root, parent, map[string]any{})
	}
	if parent.Kind == syntax.Array {
		if parent.Style == "tables" {
			if len(parent.Items) == 1 {
				return f.Replace(src, root, parent, []any{})
			}
			return ownedEdits(target), nil
		}
		index := parent.Index(target)
		prev := parent.Start + 1
		next := parent.End - 1
		if index > 0 {
			prev = parent.Items[index-1].End
		}
		if index+1 < len(parent.Items) {
			next = parent.Items[index+1].Start
		}
		return deleteDelimited(src, parent, target.Start, target.End, index, len(parent.Items), prev, next)
	}
	if inline := enclosingInline(parent); inline != nil {
		entries := inlineEntries(inline)
		var edits []syntax.Edit
		for i := 0; i < len(entries); {
			if !descendant(entries[i].Value, target) {
				i++
				continue
			}
			first := i
			for i < len(entries) && descendant(entries[i].Value, target) {
				edits = append(edits, edit(entries[i].Start, entries[i].End, ""))
				i++
			}
			last := i - 1
			// Remove all separators internal to the selected run, and one boundary separator.
			for j := first; j < last; j++ {
				c := comma(src, entries[j].End, entries[j+1].Start)
				if c >= 0 {
					edits = append(edits, edit(c, c+1, ""))
				}
			}
			if i < len(entries) {
				c := comma(src, entries[last].End, entries[i].Start)
				if c >= 0 {
					edits = append(edits, edit(c, c+1, ""))
				}
			} else if first > 0 {
				c := comma(src, entries[first-1].End, entries[first].Start)
				if c >= 0 {
					edits = append(edits, edit(c, c+1, ""))
				}
			}
		}
		return edits, nil
	}
	e, err := parentEntry(target)
	if err != nil {
		return nil, err
	}
	if e.Start >= 0 {
		return []syntax.Edit{edit(e.Start, e.End, "")}, nil
	}
	return ownedEdits(target), nil
}
