package yaml

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

// Replace plans replacement of a selected value.
func (f Format) Replace(src []byte, root, target *syntax.Node, value any) (result []syntax.Edit, resultErr error) {
	defer func() {
		if resultErr == nil {
			result = tagEdits(src, target, value, result)
		}
	}()
	if edits, ok, e := aliasEdit(src, target, "replace", "", value, f.style); ok {
		return edits, e
	}
	text, e := encode(value)
	if target.Parent == nil && target.Kind == syntax.Null && strings.TrimSpace(string(src)) == "" {
		style := styleAt(src, nil)
		style.policy = f.style
		style = style.overrides()
		var block bool
		text, block, e = style.render(src, value, nil, 0, false)
		if e == nil && (block || strings.ContainsAny(text, "\r\n")) {
			text += style.nl
		}
	}
	if e != nil {
		return nil, e
	}
	if target.Meta["explicitMissingValue"] == true {
		indent := target.Meta["indent"].(int)
		nl := newline(src)
		prefix, suffix := "", ""
		if target.Start > 0 && src[target.Start-1] != '\n' && src[target.Start-1] != '\r' {
			prefix = nl
		}
		if target.Start < len(src) || target.Start > 0 && (src[target.Start-1] == '\n' || src[target.Start-1] == '\r') {
			suffix = nl
		}
		text = prefix + strings.Repeat(" ", indent) + ": " + text + suffix
		return []syntax.Edit{{Start: target.Start, End: target.End, Text: []byte(text)}}, nil
	}
	if target.Meta["flowMissingColon"] == true {
		return []syntax.Edit{{Start: target.Start, End: target.End, Text: []byte(": " + text)}}, nil
	}
	if target.Style == "multiline-plain" {
		spans := target.Meta["segments"].([]segment)
		edits := make([]syntax.Edit, 0, len(spans))
		for i, s := range spans {
			var content []byte
			if i == 0 {
				content = []byte(text)
			}
			edits = append(edits, syntax.Edit{Start: s.start, End: s.end, Text: content})
		}
		return edits, nil
	}
	if s, ok := value.(string); ok && target.Style == "single" {
		if strings.IndexFunc(s, func(r rune) bool {
			return r < 0x20 && r != '\t' || r >= 0x7f && r <= 0x9f && r != 0x85 || r == 0xfffe || r == 0xffff
		}) < 0 {
			text = "'" + strings.ReplaceAll(s, "'", "''") + "'"
		}
	}
	if target.Start == target.End && target.Start < len(src) && src[target.Start] == '#' {
		text += " "
	}
	if target.End < len(src) && src[target.End] == '#' && !strings.HasSuffix(text, " ") {
		text += " "
	}
	if target.Start == target.End {
		propertyEnd := 0
		if tag, ok := target.Meta["tag"].(tagInfo); ok && tag.end == target.Start {
			text = " " + text
		}
		if tag, ok := target.Meta["tag"].(tagInfo); ok {
			propertyEnd = tag.end
		}
		if anchorEnd, ok := target.Meta["anchorEnd"].(int); ok && anchorEnd == target.Start {
			text = " " + text
		}
		if anchorEnd, ok := target.Meta["anchorEnd"].(int); ok && anchorEnd > propertyEnd {
			propertyEnd = anchorEnd
		}
		if target.Parent != nil && target.Start > 0 && (src[target.Start-1] == ':' || src[target.Start-1] == '-') {
			text = " " + text
		}
		if target.Parent == nil {
			inlineProperties := propertyEnd > 0 && propertyEnd <= target.Start && !bytes.ContainsAny(src[propertyEnd:target.Start], "\r\n")
			if target.Start > 0 && src[target.Start-1] != '\r' && src[target.Start-1] != '\n' && !inlineProperties {
				text = newline(src) + text
			}
			if target.Start < len(src) && strings.HasPrefix(string(src[target.Start:]), "...") {
				text += newline(src)
			}
		}
	}
	if target.Style == "block-scalar" {
		end := target.Start
		for end < len(src) && src[end] != ' ' && src[end] != '\t' && src[end] != '\r' && src[end] != '\n' {
			end++
		}
		headerEnd := target.Meta["headerEnd"].(int)
		body := headerEnd
		if body < len(src) && src[body] == '\r' {
			body++
		}
		if body < len(src) && src[body] == '\n' {
			body++
		}
		if target.End == len(src) && len(src) > 0 && src[len(src)-1] != '\n' && src[len(src)-1] != '\r' {
			body = headerEnd
		}
		edits := []syntax.Edit{{Start: target.Start, End: end, Text: []byte(text)}}
		if body < target.End {
			edits = append(edits, syntax.Edit{Start: body, End: target.End})
		}
		return edits, nil
	}
	if target.Style == "block" && target.End > target.Start && (src[target.End-1] == '\n' || src[target.End-1] == '\r') {
		text += newline(src)
	}
	return []syntax.Edit{{Start: target.Start, End: target.End, Text: []byte(text)}}, nil
}
func encode(value any) (string, error) {
	text, e := syntax.JSON(value)
	if e != nil {
		return "", e
	}
	var b strings.Builder
	for _, r := range text {
		if r >= 0x7f && r <= 0x9f && r != 0x85 || r == 0xfffe || r == 0xffff {
			fmt.Fprintf(&b, `\u%04x`, r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String(), nil
}
func newline(src []byte) string {
	if bytes.Contains(src, []byte("\r\n")) {
		return "\r\n"
	}
	if bytes.IndexByte(src, '\r') >= 0 && bytes.IndexByte(src, '\n') < 0 {
		return "\r"
	}
	return "\n"
}

// Insert plans a missing object member or an array append.
func (f Format) Insert(src []byte, root, parent *syntax.Node, key string, value any) ([]syntax.Edit, error) {
	if edits, ok, e := aliasEdit(src, parent, "insert", key, value, f.style); ok {
		return edits, e
	}
	if parent.Kind != syntax.Object && parent.Kind != syntax.Array {
		return nil, fmt.Errorf("yaml: cannot insert into %s", parent.Kind)
	}
	if parent.Kind == syntax.Object && parent.Get(key) != nil {
		return nil, fmt.Errorf("yaml: mapping key already exists")
	}
	if parent.Kind == syntax.Array && key != "" && key != "-" && key != strconv.Itoa(len(parent.Items)) {
		return nil, fmt.Errorf("yaml: array insertion only supports append")
	}
	if parent.Style == "flow-pair" {
		object := parent.Interface().(map[string]any)
		object[key] = value
		return f.Replace(src, root, parent, object)
	}
	style := styleAt(src, parent)
	style.policy = f.style
	style = style.overrides()
	template := insertionTemplate(src, parent, value)
	if parent.Style == "flow" {
		return insertFlow(src, parent, key, value, style, template)
	}
	indent, ok := parent.Meta["indent"].(int)
	if !ok {
		return nil, fmt.Errorf("yaml: container has no insertion location")
	}
	var pos int
	if parent.Kind == syntax.Object {
		entry := parent.Entries[len(parent.Entries)-1]
		pos = entry.Meta["deleteEnd"].(int)
	} else {
		pos = parent.Items[len(parent.Items)-1].Meta["itemEnd"].(int)
	}
	nl := style.nl
	prefix := ""
	if pos > 0 && src[pos-1] != '\n' && src[pos-1] != '\r' {
		prefix = nl
	}
	suffix := nl
	if pos == len(src) && len(src) > 0 && src[len(src)-1] != '\n' && src[len(src)-1] != '\r' {
		suffix = ""
	}
	valueIndent := indent + style.step
	gap := sequenceGap(src, parent)
	compact := compactCollection(template, valueKind(value))
	if parent.Kind == syntax.Array && compact {
		valueIndent = indent + 1 + len(gap)
	}
	text, block, e := style.render(src, value, template, valueIndent, false)
	if e != nil {
		return nil, e
	}
	leader := strings.Repeat(" ", indent)
	if parent.Kind == syntax.Object {
		leader += styledString(key, style.key, true, false)
		if block {
			leader += strings.TrimRight(style.colon, " \t") + nl + strings.Repeat(" ", valueIndent)
		} else {
			leader += separatorFor(styledString(key, style.key, true, false), style.colon, false)
		}
	} else {
		if block && !compact {
			leader += "-" + nl + strings.Repeat(" ", valueIndent)
		} else {
			leader += "-" + gap
		}
	}
	if strings.ContainsAny(text, "\r\n") && suffix == "" {
		suffix = nl
	}
	return []syntax.Edit{{Start: pos, End: pos, Text: []byte(prefix + leader + text + suffix)}}, nil
}
func comma(src []byte, start, end int) int {
	for i := start; i < end; i++ {
		if src[i] == '#' {
			for i < end && src[i] != '\n' && src[i] != '\r' {
				i++
			}
			continue
		}
		if src[i] == ',' {
			return i
		}
	}
	return -1
}

// Delete plans removal of a node and its required syntax.
func (f Format) Delete(src []byte, root, target *syntax.Node) ([]syntax.Edit, error) {
	if edits, ok, e := aliasEdit(src, target, "delete", "", nil, f.style); ok {
		return edits, e
	}
	parent := target.Parent
	if parent == nil {
		return f.Replace(src, root, target, nil)
	}
	start, end := target.Start, target.End
	if anchorStart, ok := target.Meta["anchorStart"].(int); ok && parent.Kind == syntax.Array {
		start = anchorStart
	}
	if tag, ok := target.Meta["tag"].(tagInfo); ok && parent.Kind == syntax.Array && tag.start < start {
		start = tag.start
	}
	idx := parent.Index(target)
	count := len(parent.Items)
	if parent.Kind == syntax.Object {
		entry := parent.Entry(target)
		if entry == nil {
			return nil, fmt.Errorf("yaml: node is not in its parent")
		}
		start, end = entry.Start, entry.End
		count = len(parent.Entries)
		for i, e := range parent.Entries {
			if e.Value == target {
				idx = i
				break
			}
		}
	}
	if parent.Style == "flow-pair" {
		return []syntax.Edit{{Start: parent.Start, End: parent.End, Text: []byte("{}")}}, nil
	}
	if parent.Style == "flow" {
		edits := []syntax.Edit{{Start: start, End: end}}
		if count == 1 {
			if c := comma(src, end, parent.End-1); c >= 0 {
				edits = append(edits, syntax.Edit{Start: c, End: c + 1})
			}
		} else if idx < count-1 {
			c := comma(src, end, parent.End)
			if c < 0 {
				return nil, fmt.Errorf("yaml: missing collection separator")
			}
			edits = append(edits, syntax.Edit{Start: c, End: c + 1})
		} else if idx > 0 {
			prevEnd := 0
			if parent.Kind == syntax.Object {
				prevEnd = parent.Entries[idx-1].End
			} else {
				prevEnd = parent.Items[idx-1].End
			}
			c := comma(src, prevEnd, start)
			if c < 0 {
				return nil, fmt.Errorf("yaml: missing collection separator")
			}
			edits = append(edits, syntax.Edit{Start: c, End: c + 1})
		}
		return edits, nil
	}
	var lineStart, lineEnd int
	if parent.Kind == syntax.Object {
		e := parent.Entry(target)
		lineStart = e.Meta["lineStart"].(int)
		lineEnd = e.Meta["deleteEnd"].(int)
	} else {
		lineStart = target.Meta["itemStart"].(int)
		lineEnd = target.Meta["itemEnd"].(int)
	}
	// A compact first mapping entry shares the line with the sequence dash. Removing
	// it would also remove the dash; retain that line and put the next entry on it.
	if count > 1 && parent.Kind == syntax.Object && parent.Parent != nil && parent.Parent.Kind == syntax.Array && idx == 0 {
		headerEnd := parent.Entries[0].Meta["headerEnd"].(int)
		if end < headerEnd && strings.Contains(string(src[end:headerEnd]), "#") {
			return []syntax.Edit{{Start: start, End: end}}, nil
		}
		next := parent.Entries[1]
		return []syntax.Edit{{Start: start, End: next.Start}}, nil
	}
	if count > 1 && parent.Kind == syntax.Array && parent.Meta["compact"] == true && idx == 0 {
		headerEnd := target.End
		for headerEnd < len(src) && src[headerEnd] != '\r' && src[headerEnd] != '\n' {
			headerEnd++
		}
		if strings.Contains(string(src[target.End:headerEnd]), "#") {
			return []syntax.Edit{{Start: parent.Start, End: target.End}}, nil
		}
		next := parent.Items[1]
		end := next.Meta["itemStart"].(int) + parent.Meta["indent"].(int)
		return []syntax.Edit{{Start: parent.Start, End: end}}, nil
	}
	if count == 1 {
		empty := "[]"
		if parent.Kind == syntax.Object {
			empty = "{}"
		}
		if parent.End < len(src) && src[parent.End] == '#' {
			empty += " "
		}
		if parent.End > parent.Start && (src[parent.End-1] == '\n' || src[parent.End-1] == '\r') {
			empty += newline(src)
		}
		return []syntax.Edit{{Start: parent.Start, End: parent.End, Text: []byte(empty)}}, nil
	}
	// Keep an inline comment as its own comment line when its scalar is removed.
	comment := -1
	var headerEnd int
	if parent.Kind == syntax.Object {
		headerEnd = parent.Entry(target).Meta["headerEnd"].(int)
	} else {
		for headerEnd = lineStart; headerEnd < len(src) && src[headerEnd] != '\n' && src[headerEnd] != '\r'; headerEnd++ {
		}
	}
	scan := target.End
	if scan > headerEnd {
		scan = headerEnd
	}
	for i := scan; i < headerEnd; i++ {
		if src[i] == '#' {
			comment = i
			break
		}
	}
	replacement := ""
	if comment >= 0 {
		replacement = string(src[lineStart:lineStart+indentation(src, lineStart)]) + string(src[comment:headerEnd]) + newline(src)
	}
	return []syntax.Edit{{Start: lineStart, End: lineEnd, Text: []byte(replacement)}}, nil
}
func indentation(src []byte, start int) int {
	i := start
	for i < len(src) && src[i] == ' ' {
		i++
	}
	return i - start
}
