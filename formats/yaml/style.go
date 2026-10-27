package yaml

import (
	"slices"
	"sort"
	"strings"

	scalarstyle "github.com/vasileknik76/tql/internal/style"
	"github.com/vasileknik76/tql/syntax"
)

// insertionStyle describes only reusable whitespace and quoting. Source comments,
// properties, and scalar contents are never part of a style template.
type insertionStyle struct {
	key, str, colon, comma, nl string
	step                       int
	policy                     syntax.Style
}

func styleAt(src []byte, parent *syntax.Node) insertionStyle {
	s := insertionStyle{key: "double", str: "double", colon: ": ", comma: ", ", nl: newline(src), step: 2}
	keySet, strSet, colonSet, stepSet, commaSet := false, false, false, false, false
	for n := parent; n != nil; n = n.Parent {
		for i := len(n.Entries) - 1; i >= 0 && (!keySet || !colonSet); i-- {
			e := n.Entries[i]
			if !keySet {
				s.key = keyStyle(src, e)
				keySet = true
			}
			if !colonSet {
				if c := entryColon(src, e); c != "" {
					s.colon = c
					colonSet = true
				}
			}
		}
		for i := 0; i < childCount(n); i++ {
			v := reverseChild(n, i)
			if !strSet && v.Kind == syntax.String && (v.Style == "single" || v.Style == "double" || v.Style == "plain") {
				s.str = v.Style
				strSet = true
			}
			if !stepSet && v.Style == "block" {
				a, aok := n.Meta["indent"].(int)
				b, bok := v.Meta["indent"].(int)
				if aok && bok && b > a {
					s.step = b - a
					stepSet = true
				}
			}
			if strSet && stepSet {
				break
			}
		}
		if !stepSet && n.Style == "block" && n.Parent != nil {
			a, aok := n.Parent.Meta["indent"].(int)
			b, bok := n.Meta["indent"].(int)
			if aok && bok && b > a {
				s.step = b - a
				stepSet = true
			}
		}
		if n.Style == "flow" && !commaSet {
			s.comma = flowSeparator(src, n, s)
			commaSet = true
		}
	}
	return s
}
func childCount(n *syntax.Node) int { return len(n.Items) + len(n.Entries) }
func reverseChild(n *syntax.Node, i int) *syntax.Node {
	if i < len(n.Items) {
		return n.Items[len(n.Items)-1-i]
	}
	return n.Entries[len(n.Entries)-1-(i-len(n.Items))].Value
}
func keyStyle(src []byte, e *syntax.Entry) string {
	if e.KeyStart >= 0 && e.KeyStart < len(src) {
		switch src[e.KeyStart] {
		case '\'':
			return "single"
		case '"':
			return "double"
		}
	}
	return "plain"
}
func entryColon(src []byte, e *syntax.Entry) string {
	if e.KeyEnd < 0 || e.Value.Start < e.KeyEnd || e.Value.Start > len(src) {
		return ""
	}
	gap := string(src[e.KeyEnd:e.Value.Start])
	if strings.ContainsAny(gap, "\r\n#&!") || strings.Count(gap, ":") != 1 || strings.Trim(gap, " \t:") != "" {
		return ""
	}
	return gap
}
func printableSingle(s string) bool {
	return !strings.ContainsAny(s, "\r\n") && strings.IndexFunc(s, func(r rune) bool {
		return r < 0x20 && r != '\t' || r >= 0x7f && r <= 0x9f && r != 0x85 || r == 0xfffe || r == 0xffff
	}) < 0
}
func styledString(s, style string, key, flow bool) string {
	if style == "single" && printableSingle(s) {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	if style == "plain" && safePlain(s, key, flow) {
		return s
	}
	text, _ := encode(s)
	return text
}
func safePlain(s string, key, inFlow bool) bool {
	if s == "" || strings.TrimSpace(s) != s || strings.ContainsAny(s, "\r\n\t") || strings.ContainsRune("-?:,[]{}#&*!|>'\"%@`", rune(s[0])) {
		return false
	}
	// Probe in the actual syntactic context. This also rejects implicit numbers,
	// booleans, comments, document markers, and punctuation with special meaning.
	var text string
	if key {
		if inFlow {
			text = "{" + s + ": 0}"
		} else {
			text = s + ": 0\n"
		}
	} else if inFlow {
		text = "[" + s + "]"
	} else {
		text = "x: " + s + "\n"
	}
	n, e := (Format{}).Parse([]byte(text))
	if e != nil {
		return false
	}
	if key {
		return n.Kind == syntax.Object && len(n.Entries) == 1 && n.Entries[0].Key == s
	}
	var v *syntax.Node
	if inFlow {
		if n.Kind != syntax.Array || len(n.Items) != 1 {
			return false
		}
		v = n.Items[0]
	} else {
		if n.Kind != syntax.Object || len(n.Entries) != 1 {
			return false
		}
		v = n.Get("x")
	}
	return v != nil && v.Kind == syntax.String && v.Value == s
}
func valueKind(value any) syntax.Kind { return scalarstyle.Kind(value) }

func insertionTemplate(src []byte, parent *syntax.Node, value any) *syntax.Node {
	kind := valueKind(value)
	return syntax.NearbyTemplate(parent, kind, func(n *syntax.Node) bool {
		return kind != syntax.Array || scalarstyle.ObjectArrayNode(n) == scalarstyle.ObjectArray(value)
	}, func(v *syntax.Node) string { return scalarstyle.Signature(src, v) })
}

func childTemplate(src []byte, n *syntax.Node, key string, v any) *syntax.Node {
	if n == nil {
		return nil
	}
	if x := n.Get(key); x != nil && x.Kind == valueKind(v) {
		return x
	}
	return insertionTemplate(src, n, v)
}

func flowSpans(n *syntax.Node) [][2]int {
	spans := make([][2]int, 0, len(n.Items)+len(n.Entries))
	for _, v := range n.Items {
		spans = append(spans, [2]int{v.Start, v.End})
	}
	for _, e := range n.Entries {
		spans = append(spans, [2]int{e.Start, e.End})
	}
	return spans
}
func horizontal(s string) bool { return strings.Trim(s, " \t") == "" }
func sourceIndent(src []byte, pos int) string {
	start := pos
	for start > 0 && src[start-1] != '\n' && src[start-1] != '\r' {
		start--
	}
	end := start
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return string(src[start:end])
}
func flowSeparator(src []byte, n *syntax.Node, s insertionStyle) string {
	spans := flowSpans(n)
	if len(spans) > 1 {
		prev, last := spans[len(spans)-2], spans[len(spans)-1]
		gap := string(src[prev[1]:last[0]])
		if !strings.Contains(gap, "#") && strings.Trim(gap, " \t\r\n,") == "" {
			return gap
		}
	}
	if len(spans) > 0 {
		gap := string(src[n.Start+1 : spans[0][0]])
		if strings.ContainsAny(gap, "\r\n") {
			return "," + s.nl + sourceIndent(src, spans[0][0])
		}
		if horizontal(gap) {
			return "," + gap
		}
	}
	return ", "
}
func orderedKeys(m map[string]any, t *syntax.Node) []string {
	keys := make([]string, 0, len(m))
	seen := map[string]bool{}
	if t != nil {
		for _, e := range t.Entries {
			if _, ok := m[e.Key]; ok {
				keys = append(keys, e.Key)
				seen[e.Key] = true
			}
		}
	}
	rest := make([]string, 0, len(m))
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}
func (s insertionStyle) withTemplate(src []byte, t *syntax.Node) insertionStyle {
	if t == nil {
		return s
	}
	for i, v := range slices.Backward(t.Entries) {
		if i == len(t.Entries)-1 {
			s.key = keyStyle(src, v)
		}
		if c := entryColon(src, v); c != "" {
			s.colon = c
			break
		}
	}
	for i := 0; i < childCount(t); i++ {
		v := reverseChild(t, i)
		if v.Kind == syntax.String && (v.Style == "plain" || v.Style == "single" || v.Style == "double") {
			s.str = v.Style
			break
		}
	}
	if t.Style == "flow" {
		s.comma = flowSeparator(src, t, s)
	}
	return s
}
func (s insertionStyle) render(src []byte, value any, t *syntax.Node, indent int, forceFlow bool) (string, bool, error) {
	s = s.withTemplate(src, t).overrides()
	if str, ok := value.(string); ok {
		style := s.str
		if (s.policy.Strings == "" || s.policy.Strings == "auto") && t != nil && (t.Style == "single" || t.Style == "double" || t.Style == "plain") {
			style = t.Style
		}
		if s.policy.Strings != "" && s.policy.Strings != "auto" {
			style = s.policy.Strings
		}
		if (s.policy.Strings == "" || s.policy.Strings == "auto") && t != nil && t.Style == "block-scalar" {
			style = "literal"
			if src[t.Start] == '>' {
				style = "folded"
			}
		}
		if !forceFlow && (style == "literal" || style == "folded") {
			if text, ok := s.blockString(str, style, indent); ok {
				return text, false, nil
			}
		}
		return styledString(str, style, false, forceFlow), false, nil
	}
	kind := valueKind(value)
	if kind != syntax.Object && kind != syntax.Array {
		sample := ""
		if t != nil && t.Start >= 0 && t.End <= len(src) {
			sample = string(src[t.Start:t.End])
		}
		if text, ok := scalarstyle.Integer(value, s.policy.Numbers, sample); ok {
			return text, false, nil
		}
		text, e := encode(value)
		if t != nil && (value == nil || kind == syntax.Bool) {
			if value == nil && sample == "~" {
				text = "~"
			} else if sample != "" && sample == strings.ToUpper(sample) {
				text = strings.ToUpper(text)
			} else if len(sample) > 0 && sample[0] >= 'A' && sample[0] <= 'Z' {
				text = strings.ToUpper(text[:1]) + text[1:]
			}
		}
		return text, false, e
	}
	layout := s.policy.Objects
	if kind == syntax.Array {
		layout = s.policy.Arrays
	}
	block := !forceFlow && (layout == "block" || (layout == "" || layout == "auto") && (t == nil || t.Style == "block"))
	if kind == syntax.Object && len(value.(map[string]any)) == 0 || kind == syntax.Array && len(value.([]any)) == 0 {
		text, e := encode(value)
		return text, false, e
	}
	pad := strings.Repeat(" ", indent)
	var parts []string
	if kind == syntax.Object {
		m := value.(map[string]any)
		for _, k := range orderedKeys(m, t) {
			ct := childTemplate(src, t, k, m[k])
			childIndent := indent + s.step
			text, childBlock, e := s.render(src, m[k], ct, childIndent, !block)
			if e != nil {
				return "", false, e
			}
			quoteStyle := s.key
			if (s.policy.Keys == "" || s.policy.Keys == "auto") && t != nil {
				for _, entry := range t.Entries {
					if entry.Key == k {
						quoteStyle = keyStyle(src, entry)
						break
					}
				}
			}
			key := styledString(k, quoteStyle, true, !block)
			if childBlock {
				parts = append(parts, key+strings.TrimRight(s.colon, " \t")+s.nl+strings.Repeat(" ", childIndent)+text)
			} else {
				parts = append(parts, key+separatorFor(key, s.colon, !block)+text)
			}
		}
	} else {
		for _, v := range value.([]any) {
			ct := childTemplate(src, t, "", v)
			childIndent := indent + s.step
			compact := compactCollection(ct, valueKind(v))
			gap := " "
			if t != nil {
				gap = sequenceGap(src, t)
			}
			if compact {
				childIndent = indent + 1 + len(gap)
			}
			text, childBlock, e := s.render(src, v, ct, childIndent, !block)
			if e != nil {
				return "", false, e
			}
			if block {
				if childBlock && !compact {
					parts = append(parts, "-"+s.nl+strings.Repeat(" ", childIndent)+text)
				} else {
					parts = append(parts, "-"+gap+text)
				}
			} else {
				parts = append(parts, text)
			}
		}
	}
	if block {
		return strings.Join(parts, s.nl+pad), true, nil
	}
	open, closing := "[", "]"
	if kind == syntax.Object {
		open, closing = "{", "}"
	}
	// New nested flow values use the template's horizontal padding/separators.
	// Multiline templates receive indentation relative to the new location.
	sep := s.comma
	left, right := "", ""
	if t != nil && t.Style == "flow" {
		spans := flowSpans(t)
		if len(spans) > 0 {
			a, b := string(src[t.Start+1:spans[0][0]]), string(src[spans[len(spans)-1][1]:t.End-1])
			if horizontal(a) {
				left = a
			}
			if horizontal(b) {
				right = b
			}
			if strings.ContainsAny(a, "\r\n") {
				left = s.nl + pad
				right = s.nl + strings.Repeat(" ", max(0, indent-s.step))
				sep = "," + s.nl + pad
			}
			if c := comma(src, spans[len(spans)-1][1], t.End-1); c >= 0 {
				right = "," + right
			}
		}
	}
	if strings.ContainsAny(sep, "\r\n") && left == "" {
		sep = ", "
	}
	return open + left + strings.Join(parts, sep) + right + closing, false, nil
}
func sequenceGap(src []byte, n *syntax.Node) string {
	for i := 0; i < childCount(n); i++ {
		v := reverseChild(n, i)
		start, ok := v.Meta["itemStart"].(int)
		if !ok {
			continue
		}
		indent, ok := n.Meta["indent"].(int)
		if !ok {
			continue
		}
		start += indent + 1
		if start <= v.Start && v.Start <= len(src) {
			gap := string(src[start:v.Start])
			if gap != "" && horizontal(gap) {
				return gap
			}
		}
	}
	return " "
}
func compactCollection(t *syntax.Node, kind syntax.Kind) bool {
	if t != nil && t.Style == "block" {
		return t.Meta["compact"] == true
	}
	return kind == syntax.Object
}
func separatorFor(key, colon string, inFlow bool) string {
	if (!inFlow || key != "" && key[0] != '\'' && key[0] != '"') && !strings.HasSuffix(colon, " ") && !strings.HasSuffix(colon, "\t") {
		return colon + " "
	}
	return colon
}

func insertFlow(src []byte, parent *syntax.Node, key string, value any, s insertionStyle, t *syntax.Node) ([]syntax.Edit, error) {
	text, _, e := s.render(src, value, t, len(sourceIndent(src, parent.Start))+s.step, true)
	if e != nil {
		return nil, e
	}
	if parent.Kind == syntax.Object {
		keyText := styledString(key, s.key, true, true)
		text = keyText + separatorFor(keyText, s.colon, true) + text
	}
	spans := flowSpans(parent)
	closing := parent.End - 1
	if len(spans) == 0 {
		gap := string(src[parent.Start+1 : closing])
		if strings.Contains(gap, "#") {
			pos := closing
			for pos > parent.Start+1 && src[pos-1] != '\n' && src[pos-1] != '\r' {
				pos--
			}
			indent := sourceIndent(src, parent.Start) + strings.Repeat(" ", s.step)
			return []syntax.Edit{{Start: pos, End: pos, Text: []byte(indent + text + s.nl)}}, nil
		}
		prefix := gap
		if !horizontal(gap) {
			prefix = s.nl + sourceIndent(src, parent.Start) + strings.Repeat(" ", s.step)
		}
		return []syntax.Edit{{Start: parent.Start + 1, End: parent.Start + 1, Text: []byte(prefix + text)}}, nil
	}
	last := spans[len(spans)-1]
	c := comma(src, last[1], closing)
	tail := string(src[last[1]:closing])
	if strings.Contains(tail, "#") {
		// Leave comments on their original lines. Add a separator at the old value,
		// and append the new value on its own line before the closing delimiter.
		pos := closing
		for pos > last[1] && src[pos-1] != '\n' && src[pos-1] != '\r' {
			pos--
		}
		if pos <= last[1] {
			pos = closing
		}
		indent := sourceIndent(src, last[0])
		if !strings.ContainsAny(string(src[parent.Start+1:last[0]]), "\r\n") {
			indent = sourceIndent(src, parent.Start) + strings.Repeat(" ", s.step)
		}
		content := indent + text
		if c >= 0 {
			content += ","
		}
		content += s.nl
		edits := []syntax.Edit{}
		if c < 0 {
			edits = append(edits, syntax.Edit{Start: last[1], End: last[1], Text: []byte(",")})
		}
		return append(edits, syntax.Edit{Start: pos, End: pos, Text: []byte(content)}), nil
	}
	if c >= 0 {
		sep := strings.TrimPrefix(s.comma, ",")
		return []syntax.Edit{{Start: c + 1, End: c + 1, Text: []byte(sep + text + ",")}}, nil
	}
	return []syntax.Edit{{Start: last[1], End: last[1], Text: []byte(s.comma + text)}}, nil
}
