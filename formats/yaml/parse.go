// Package yaml implements a source-aware YAML 1.2 configuration profile.
// The package README documents supported constructs and compatibility limits.
package yaml

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vasileknik76/tql/syntax"
)

// Format parses and edits YAML documents without reformatting untouched source.
type Format struct{}

var _ syntax.Format = Format{}

type line struct {
	start, end, next, indent int
	text                     string
}
type parser struct {
	src         []byte
	lines       []line
	i, depth    int
	anchors     map[string]*syntax.Node
	aliasNodes  int
	forceString bool
}
type segment struct{ start, end int }

func errAt(pos int, msg string) error { return fmt.Errorf("yaml: byte %d: %s", pos, msg) }

// Parse reads one document and returns a linked, source-aware syntax tree.
// It does not modify source.
func (Format) Parse(src []byte) (*syntax.Node, error) {
	if !utf8.Valid(src) {
		return nil, errAt(0, "invalid UTF-8")
	}
	for i, c := range src {
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			return nil, errAt(i, "non-printable character")
		}
	}
	for i, r := range string(src) {
		if r >= 0x7f && r <= 0x9f && r != 0x85 || r == 0xfffe || r == 0xffff {
			return nil, errAt(i, "non-printable character")
		}
	}
	p := &parser{src: src}
	offset := 0
	if strings.HasPrefix(string(src), "\xef\xbb\xbf") {
		offset = 3
	}
	for off := offset; off < len(src); {
		end := off
		for end < len(src) && src[end] != '\n' && src[end] != '\r' {
			end++
		}
		next := end
		if next < len(src) {
			next++
			if src[end] == '\r' && next < len(src) && src[next] == '\n' {
				next++
			}
		}
		in := 0
		for off+in < end && src[off+in] == ' ' {
			in++
		}
		p.lines = append(p.lines, line{off, end, next, in, string(src[off+in : end])})
		off = next
	}
	p.skip()
	hadDirective := false
	for p.i < len(p.lines) && strings.HasPrefix(p.lines[p.i].text, "%") {
		if strings.TrimSpace(p.lines[p.i].text) != "%YAML 1.2" {
			return nil, errAt(p.lines[p.i].start, "unsupported directive")
		}
		if hadDirective {
			return nil, errAt(p.lines[p.i].start, "duplicate YAML directive")
		}
		hadDirective = true
		p.i++
		p.skip()
	}
	if p.i < len(p.lines) && marker(p.lines[p.i], "---") {
		p.i++
		p.skip()
	} else if hadDirective {
		return nil, errAt(0, "a YAML directive requires ---")
	}
	var n *syntax.Node
	var e error
	if p.i == len(p.lines) || marker(p.lines[p.i], "...") {
		pos := len(src)
		if p.i < len(p.lines) {
			pos = p.lines[p.i].start
		}
		n = &syntax.Node{Kind: syntax.Null, Start: pos, End: pos}
	} else {
		n, e = p.block(p.lines[p.i].indent)
		if e != nil {
			return nil, e
		}
	}
	p.skip()
	if p.i < len(p.lines) && marker(p.lines[p.i], "...") {
		p.i++
		p.skip()
	}
	if p.i < len(p.lines) {
		return nil, errAt(p.lines[p.i].start, "unexpected content or multiple YAML documents")
	}
	syntax.Link(n)
	return n, nil
}
func marker(l line, s string) bool {
	return l.indent == 0 && (l.text == s || strings.HasPrefix(l.text, s+" ") && strings.HasPrefix(strings.TrimSpace(l.text[len(s):]), "#"))
}
func (p *parser) skip() {
	for p.i < len(p.lines) && (strings.TrimSpace(p.lines[p.i].text) == "" || strings.HasPrefix(p.lines[p.i].text, "#")) {
		p.i++
	}
}
func seq(s string) bool { return s == "-" || strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "-\t") }
func colon(s string) int {
	quote := byte(0)
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if quote == '"' && c == '\\' {
				i++
				continue
			}
			if c == quote {
				if quote == '\'' && i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			if i == 0 || depth > 0 && (i == 0 || strings.ContainsRune("[{: ,\t", rune(s[i-1]))) {
				quote = c
			}
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case '#':
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return -1
			}
		case ':':
			if depth == 0 && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t') {
				return i
			}
		}
	}
	return -1
}
func (p *parser) block(indent int) (*syntax.Node, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 256 {
		return nil, errAt(p.lines[p.i].start, "nesting limit exceeded")
	}
	l := p.lines[p.i]
	if strings.HasPrefix(l.text, "\t") {
		return nil, errAt(l.start+l.indent, "tab indentation is not allowed")
	}
	if seq(l.text) {
		return p.sequence(indent)
	}
	if colon(l.text) >= 0 {
		return p.mapping(indent, -1)
	}
	p.i++
	valueIndent := indent
	if p.src[l.start+l.indent] != '|' && p.src[l.start+l.indent] != '>' {
		valueIndent--
	}
	n, e := p.value(l.start+l.indent, l.end, valueIndent)
	if e != nil {
		return nil, e
	}
	anchor, hasAnchor := pending(n)
	tag, hasTag := pendingTag(n)
	if hasAnchor || hasTag {
		save := p.i
		p.skip()
		if p.i < len(p.lines) && p.lines[p.i].indent >= indent && !marker(p.lines[p.i], "---") && !marker(p.lines[p.i], "...") {
			n, e = p.block(p.lines[p.i].indent)
			if e != nil {
				return nil, e
			}
		} else {
			p.i = save
		}
		if hasTag {
			if e = p.applyTag(n, tag); e != nil {
				return nil, e
			}
		}
		if hasAnchor {
			if e = p.bind(n, anchor); e != nil {
				return nil, e
			}
		}
	}
	p.skip()
	if p.i < len(p.lines) && p.lines[p.i].indent > indent {
		return nil, errAt(p.lines[p.i].start, "unexpected indented content")
	}
	return n, nil
}
func (p *parser) mapping(indent, first int) (*syntax.Node, error) {
	n := &syntax.Node{Kind: syntax.Object, Style: "block", Meta: map[string]any{"indent": indent}}
	seen := map[string]bool{}
	n.Start = -1
	for {
		p.skip()
		if p.i >= len(p.lines) {
			break
		}
		l := p.lines[p.i]
		if strings.HasPrefix(l.text, "\t") {
			return nil, errAt(l.start+l.indent, "tab indentation is not allowed")
		}
		start := l.start + l.indent
		if first >= 0 {
			start = first
			first = -1
		} else if l.indent != indent || seq(l.text) || marker(l, "---") || marker(l, "...") {
			break
		}
		s := string(p.src[start:l.end])
		c := colon(s)
		if c < 0 {
			return nil, errAt(start, "expected a mapping key followed by ':'")
		}
		keyEnd := start + c
		for keyEnd > start && (p.src[keyEnd-1] == ' ' || p.src[keyEnd-1] == '\t') {
			keyEnd--
		}
		if keyEnd == start {
			return nil, errAt(start, "empty mapping keys are unsupported")
		}
		if strings.ContainsRune("&*!", rune(p.src[start])) {
			return nil, errAt(start, "node properties and aliases in mapping keys are unsupported")
		}
		keyParser := &flow{p: p, pos: start, limit: keyEnd}
		keyNode, e := keyParser.node(false)
		if e != nil {
			return nil, e
		}
		if keyParser.pos != keyEnd {
			return nil, errAt(keyParser.pos, "unexpected mapping key content")
		}
		if keyNode.Kind == syntax.Array || keyNode.Kind == syntax.Object {
			return nil, errAt(start, "complex mapping keys are unsupported")
		}
		key := string(p.src[start:keyEnd])
		if keyNode.Kind == syntax.String {
			key = keyNode.Value.(string)
		}
		if key == "<<" {
			return nil, errAt(start, "merge keys are unsupported")
		}
		if seen[key] {
			return nil, errAt(start, "duplicate mapping key "+strconv.Quote(key))
		}
		seen[key] = true
		pos := start + c + 1
		for pos < l.end && (p.src[pos] == ' ' || p.src[pos] == '\t') {
			pos++
		}
		p.i++
		v, e := p.value(pos, l.end, indent)
		if e != nil {
			return nil, e
		}
		anchor, hasAnchor := pending(v)
		tag, hasTag := pendingTag(v)
		if v.Kind == syntax.Null && v.Start == v.End {
			save := p.i
			p.skip()
			if p.i < len(p.lines) && (p.lines[p.i].indent > indent || (p.lines[p.i].indent == indent && seq(p.lines[p.i].text))) {
				v, e = p.block(p.lines[p.i].indent)
				if e != nil {
					return nil, e
				}
				v.Meta = meta(v.Meta)
				v.Meta["nested"] = true
			} else {
				p.i = save
			}
		}
		if hasTag {
			if e = p.applyTag(v, tag); e != nil {
				return nil, e
			}
		}
		if hasAnchor {
			if e = p.bind(v, anchor); e != nil {
				return nil, e
			}
		}
		end := v.End
		entry := &syntax.Entry{Key: key, KeyStart: start, KeyEnd: keyEnd, Start: start, End: end, Value: v, Meta: map[string]any{"lineStart": l.start, "lineEnd": l.next, "headerEnd": l.end}}
		entry.Meta["deleteEnd"] = p.extentEnd(end, l.next)
		n.Entries = append(n.Entries, entry)
		if n.Start < 0 {
			n.Start = start
		}
		n.End = end
	}
	if len(n.Entries) == 0 {
		return nil, errAt(0, "empty block mapping")
	}
	return n, nil
}
func meta(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
func (p *parser) extentEnd(end, fallback int) int {
	if end > 0 && end <= len(p.src) && (p.src[end-1] == '\n' || p.src[end-1] == '\r') {
		return end
	}
	for _, l := range p.lines {
		if l.end == end || l.start <= end && end < l.end {
			return l.next
		}
	}
	if end > fallback {
		return end
	}
	return fallback
}
func (p *parser) sequence(indent int) (*syntax.Node, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 256 {
		return nil, errAt(p.lines[p.i].start, "nesting limit exceeded")
	}
	n := &syntax.Node{Kind: syntax.Array, Style: "block", Start: p.lines[p.i].start + indent, Meta: map[string]any{"indent": indent}}
	for {
		p.skip()
		if p.i >= len(p.lines) {
			break
		}
		l := p.lines[p.i]
		if l.indent != indent || !seq(l.text) {
			break
		}
		pos := l.start + indent + 1
		for pos < l.end && (p.src[pos] == ' ' || p.src[pos] == '\t') {
			pos++
		}
		var v *syntax.Node
		var e error
		if pos < l.end && seq(string(p.src[pos:l.end])) {
			original := p.lines[p.i]
			temp := original
			temp.indent = pos - l.start
			temp.text = string(p.src[pos:l.end])
			at := p.i
			p.lines[at] = temp
			v, e = p.sequence(temp.indent)
			p.lines[at] = original
			if e == nil {
				v.Meta["compact"] = true
			}
		} else if pos < l.end && colon(string(p.src[pos:l.end])) >= 0 {
			v, e = p.mapping(pos-l.start, pos)
			if e == nil {
				v.Meta["compact"] = true
			}
		} else {
			p.i++
			v, e = p.value(pos, l.end, indent)
			var anchor anchorInfo
			var tag tagInfo
			hasTag := false
			hasAnchor := false
			if e == nil {
				anchor, hasAnchor = pending(v)
				tag, hasTag = pendingTag(v)
			}
			if e == nil && v.Kind == syntax.Null && v.Start == v.End {
				save := p.i
				p.skip()
				if p.i < len(p.lines) && p.lines[p.i].indent > indent {
					v, e = p.block(p.lines[p.i].indent)
					if e == nil {
						v.Meta = meta(v.Meta)
						v.Meta["nested"] = true
					}
				} else {
					p.i = save
				}
			}
			if e == nil && hasTag {
				e = p.applyTag(v, tag)
			}
			if e == nil && hasAnchor {
				e = p.bind(v, anchor)
			}
		}
		if e != nil {
			return nil, e
		}
		v.Meta = meta(v.Meta)
		v.Meta["itemStart"] = l.start
		v.Meta["itemEnd"] = p.extentEnd(v.End, l.next)
		n.Items = append(n.Items, v)
		n.End = v.End
	}
	return n, nil
}
func (p *parser) value(start, end, indent int) (*syntax.Node, error) {
	if start >= end || p.src[start] == '#' {
		return &syntax.Node{Kind: syntax.Null, Start: start, End: start}, nil
	}
	if p.src[start] == '&' {
		a, next, e := property(p.src, start, end)
		if e != nil {
			return nil, e
		}
		for next < end && (p.src[next] == ' ' || p.src[next] == '\t') {
			next++
		}
		if next == end || p.src[next] == '#' {
			return &syntax.Node{Kind: syntax.Null, Start: next, End: next, Meta: map[string]any{"pendingAnchor": a}}, nil
		}
		n, e := p.value(next, end, indent)
		if e != nil {
			return nil, e
		}
		if _, ok := pendingTag(n); ok {
			n.Meta["pendingAnchor"] = a
			return n, nil
		}
		if e = p.bind(n, a); e != nil {
			return nil, e
		}
		return n, nil
	}
	if p.src[start] == '!' {
		tag, next, e := tagProperty(p.src, start, end)
		if e != nil {
			return nil, e
		}
		for next < end && (p.src[next] == ' ' || p.src[next] == '\t') {
			next++
		}
		if next == end || p.src[next] == '#' {
			return &syntax.Node{Kind: syntax.Null, Start: next, End: next, Meta: map[string]any{"pendingTag": tag}}, nil
		}
		oldForce := p.forceString
		if tag.name == "str" {
			p.forceString = true
		}
		n, e := p.value(next, end, indent)
		p.forceString = oldForce
		if e != nil {
			return nil, e
		}
		if _, ok := pending(n); ok {
			n.Meta["pendingTag"] = tag
			return n, nil
		}
		if e = p.applyTag(n, tag); e != nil {
			return nil, e
		}
		return n, nil
	}
	if p.src[start] == '|' || p.src[start] == '>' {
		return p.blockScalar(start, end, indent)
	}
	n, e := p.inline(start, end, false, indent)
	if e != nil {
		return nil, e
	}
	for p.i < len(p.lines) && p.lines[p.i].start < n.End {
		p.i++
	}
	if n.Style == "plain" {
		segments := []segment{{n.Start, n.End}}
		value := string(p.src[n.Start:n.End])
		continued := false
		for {
			// An inline comment ends a plain scalar; later indented text cannot
			// resume it after that comment.
			lineEnd := n.End
			for lineEnd < len(p.src) && p.src[lineEnd] != '\r' && p.src[lineEnd] != '\n' {
				if p.src[lineEnd] == '#' {
					break
				}
				lineEnd++
			}
			if lineEnd < len(p.src) && p.src[lineEnd] == '#' {
				break
			}
			j := p.i
			blanks := 0
			for j < len(p.lines) && strings.TrimSpace(p.lines[j].text) == "" {
				blanks++
				j++
			}
			if j < len(p.lines) && strings.HasPrefix(p.lines[j].text, "#") {
				break
			}
			if j >= len(p.lines) || p.lines[j].indent <= indent || marker(p.lines[j], "---") || marker(p.lines[j], "...") {
				break
			}
			l := p.lines[j]
			if seq(l.text) || colon(l.text) >= 0 {
				return nil, errAt(l.start, "invalid plain scalar continuation")
			}
			a, b := l.start+l.indent, l.end
			for k := a; k < b; k++ {
				if p.src[k] == '#' && (k == a || p.src[k-1] == ' ' || p.src[k-1] == '\t') {
					b = k
					break
				}
			}
			for b > a && (p.src[b-1] == ' ' || p.src[b-1] == '\t') {
				b--
			}
			if a == b {
				break
			}
			if blanks == 0 {
				value += " "
			} else {
				value += strings.Repeat("\n", blanks)
			}
			value += string(p.src[a:b])
			segments = append(segments, segment{a, b})
			n.End = b
			p.i = j + 1
			continued = true
		}
		if continued {
			n.Kind = syntax.String
			n.Value = value
			n.Style = "multiline-plain"
			n.Meta = meta(n.Meta)
			n.Meta["segments"] = segments
		}
	}
	return n, nil
}
