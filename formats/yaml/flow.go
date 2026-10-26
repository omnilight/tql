package yaml

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vasileknik76/tql/syntax"
)

type flow struct {
	p                 *parser
	pos, limit, depth int
	minIndent         int
	propertyDepth     int
}

func (p *parser) inline(start, end int, inFlow bool, indent int) (*syntax.Node, error) {
	f := &flow{p: p, pos: start, limit: end, depth: p.depth, minIndent: indent + 1}
	if p.src[start] == '[' || p.src[start] == '{' || p.src[start] == '\'' || p.src[start] == '"' {
		f.limit = len(p.src)
	}
	n, e := f.node(inFlow)
	if e != nil {
		return nil, e
	}
	rest := f.pos
	for rest < len(p.src) && p.src[rest] != '\r' && p.src[rest] != '\n' && (p.src[rest] == ' ' || p.src[rest] == '\t') {
		rest++
	}
	if rest < len(p.src) && p.src[rest] == '#' && rest == f.pos && (rest == 0 || p.src[rest-1] != ' ' && p.src[rest-1] != '\t') {
		return nil, errAt(rest, "comment must be separated")
	}
	if rest < len(p.src) && p.src[rest] != '\r' && p.src[rest] != '\n' && p.src[rest] != '#' {
		return nil, errAt(rest, "unexpected scalar content")
	}
	return n, nil
}
func (f *flow) ws() error {
	for f.pos < f.limit {
		c := f.p.src[f.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			f.pos++
			continue
		}
		if c == '#' {
			if f.pos > 0 && !strings.ContainsRune(" \t\r\n", rune(f.p.src[f.pos-1])) {
				return errAt(f.pos, "comment must be separated")
			}
			for f.pos < f.limit && f.p.src[f.pos] != '\n' && f.p.src[f.pos] != '\r' {
				f.pos++
			}
			continue
		}
		if f.documentMarker() {
			return errAt(f.pos, "document marker in flow collection")
		}
		start := f.pos
		for start > 0 && (f.p.src[start-1] == ' ' || f.p.src[start-1] == '\t') {
			start--
		}
		atPrefix := start == 0 || f.p.src[start-1] == '\n' || f.p.src[start-1] == '\r'
		if atPrefix && c != ']' && c != '}' && !f.continuationIndent() {
			return errAt(f.pos, "insufficient indentation in flow collection")
		}
		break
	}
	return nil
}
func (f *flow) node(inFlow bool) (*syntax.Node, error) {
	if inFlow {
		if err := f.ws(); err != nil {
			return nil, err
		}
	}
	start := f.pos
	if start >= f.limit {
		return nil, errAt(start, "expected a value")
	}
	s := f.p.src
	if strings.ContainsRune("-?:", rune(s[start])) && (start+1 == f.limit || strings.ContainsRune(" \t\r\n", rune(s[start+1])) || inFlow && strings.ContainsRune(",[]{}", rune(s[start+1]))) {
		return nil, errAt(start, "compact sequence-in-sequence notation is unsupported")
	}
	switch s[start] {
	case '*':
		a, next, e := property(s, start, f.limit)
		if e != nil {
			return nil, e
		}
		f.pos = next
		return f.p.alias(a)
	case '&':
		a, next, e := property(s, start, f.limit)
		if e != nil {
			return nil, e
		}
		a.target = f.p.beginAnchor(a)
		f.pos = next
		if err := f.ws(); err != nil {
			return nil, err
		}
		n, e := f.propertyValue(inFlow)
		if e != nil {
			return nil, e
		}
		if n, e = f.p.bind(n, a); e != nil {
			return nil, e
		}
		return n, nil
	case '!':
		tag, next, e := f.p.tagProperty(s, start, f.limit)
		if e != nil {
			return nil, e
		}
		f.pos = next
		if err := f.ws(); err != nil {
			return nil, err
		}
		n, e := f.propertyValue(inFlow)
		if e != nil {
			return nil, e
		}
		if e = f.p.applyTag(n, tag); e != nil {
			return nil, e
		}
		return n, nil
	case '@', '`', '%', ']', '}', ',', '|', '>':
		return nil, errAt(start, "unsupported scalar indicator")
	case '[', '{':
		return f.collection()
	case '\'', '"':
		return f.quoted()
	}
	return f.plain(inFlow)
}
func (f *flow) plain(inFlow bool) (*syntax.Node, error) {
	s := f.p.src
	start := f.pos
	partStart := start
	var parts []segment
	var value strings.Builder
	for f.pos < f.limit {
		c := s[f.pos]
		if c == '\n' || c == '\r' {
			if !inFlow {
				break
			}
			lineEnd := f.pos
			for lineEnd > partStart && (s[lineEnd-1] == ' ' || s[lineEnd-1] == '\t') {
				lineEnd--
			}
			breakPos := f.pos
			count := f.foldBreak()
			if f.pos == f.limit || strings.ContainsRune(",]}#", rune(s[f.pos])) || s[f.pos] == ':' && (f.pos+1 == f.limit || strings.ContainsRune(" \t\r\n,[]{}", rune(s[f.pos+1]))) {
				f.pos = breakPos
				break
			}
			if !f.continuationIndent() {
				return nil, errAt(f.pos, "insufficient indentation in plain scalar")
			}
			atLineStart := f.pos == 0 || s[f.pos-1] == '\n' || s[f.pos-1] == '\r'
			if atLineStart && f.pos+3 <= f.limit {
				token := string(s[f.pos : f.pos+3])
				if (token == "---" || token == "...") && (f.pos+3 == f.limit || strings.ContainsRune(" \t\r\n", rune(s[f.pos+3]))) {
					return nil, errAt(f.pos, "document marker in flow scalar")
				}
			}
			parts = append(parts, segment{partStart, lineEnd})
			value.Write(s[partStart:lineEnd])
			if count == 1 {
				value.WriteByte(' ')
			} else {
				value.WriteString(strings.Repeat("\n", count-1))
			}
			partStart = f.pos
			continue
		}
		if inFlow && (c == ',' || c == ']' || c == '}') {
			break
		}
		if c == '#' && (f.pos == start || s[f.pos-1] == ' ' || s[f.pos-1] == '\t') {
			break
		}
		if c == ':' && (f.pos+1 == f.limit || strings.ContainsRune(" \t\r\n", rune(s[f.pos+1])) || inFlow && strings.ContainsRune(",[]{}", rune(s[f.pos+1]))) {
			break
		}
		if inFlow && (c == '[' || c == '{') {
			return nil, errAt(f.pos, "flow indicators are not allowed in a plain scalar")
		}
		f.pos++
	}
	end := f.pos
	for end > partStart && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	if end == start {
		return emptyScalar(start), nil
	}
	if len(parts) > 0 {
		parts = append(parts, segment{partStart, end})
		value.Write(s[partStart:end])
		return &syntax.Node{Kind: syntax.String, Value: value.String(), Start: start, End: end, Style: "multiline-plain", Meta: map[string]any{"segments": parts, "scalarValue": value.String()}}, nil
	}

	return scalar(string(s[start:end]), start, end)
}
func (f *flow) propertyValue(inFlow bool) (*syntax.Node, error) {
	f.propertyDepth++
	defer func() { f.propertyDepth-- }()
	if f.propertyDepth > 256 {
		return nil, errAt(f.pos, "property nesting limit exceeded")
	}
	if f.pos >= f.limit || inFlow && (strings.ContainsRune(",]}", rune(f.p.src[f.pos])) || f.p.src[f.pos] == ':' && (f.pos+1 == f.limit || strings.ContainsRune(" \t\r\n", rune(f.p.src[f.pos+1])))) {
		return emptyScalar(f.pos), nil
	}
	return f.node(inFlow)
}
func (f *flow) quoted() (*syntax.Node, error) {
	start := f.pos
	s := f.p.src
	q := s[f.pos]
	f.pos++
	var b strings.Builder
	contentEnd := 0
	for f.pos < f.limit {
		c := s[f.pos]
		f.pos++
		if c == q {
			if q == '\'' && f.pos < f.limit && s[f.pos] == '\'' {
				b.WriteByte('\'')
				contentEnd = b.Len()
				f.pos++
				continue
			}
			return &syntax.Node{Kind: syntax.String, Value: b.String(), Start: start, End: f.pos, Style: map[byte]string{'\'': "single", '"': "double"}[q], Meta: map[string]any{"scalarValue": b.String()}}, nil
		}
		if c == '\n' || c == '\r' {
			f.pos--
			count := f.foldBreak()
			if f.documentMarker() {
				return nil, errAt(f.pos, "document marker in quoted scalar")
			}
			if !f.continuationIndent() {
				return nil, errAt(f.pos, "insufficient indentation in quoted scalar")
			}
			old := b.String()[:contentEnd]
			b.Reset()
			b.WriteString(old)
			if count == 1 {
				b.WriteByte(' ')
			} else {
				b.WriteString(strings.Repeat("\n", count-1))
			}
			contentEnd = b.Len()
			continue
		}
		if q == '"' && c == '\\' {
			if f.pos >= f.limit {
				return nil, errAt(f.pos, "unfinished escape")
			}
			c = s[f.pos]
			f.pos++
			switch c {
			case '0':
				b.WriteByte(0)
			case 'a':
				b.WriteByte(7)
			case 'b':
				b.WriteByte(8)
			case 't', '\t':
				b.WriteByte('\t')
			case 'n':
				b.WriteByte('\n')
			case 'v':
				b.WriteByte(11)
			case 'f':
				b.WriteByte(12)
			case 'r':
				b.WriteByte('\r')
			case 'e':
				b.WriteByte(27)
			case '\n', '\r':
				f.pos--
				count := f.foldBreak()
				if f.documentMarker() {
					return nil, errAt(f.pos, "document marker in quoted scalar")
				}
				if !f.continuationIndent() {
					return nil, errAt(f.pos, "insufficient indentation in quoted scalar")
				}
				if count > 1 {
					b.WriteString(strings.Repeat("\n", count-1))
				}
			case ' ':
				b.WriteByte(' ')
			case '"', '/', '\\':
				b.WriteByte(c)
			case 'N':
				b.WriteRune(0x85)
			case '_':
				b.WriteRune(0xa0)
			case 'L':
				b.WriteRune(0x2028)
			case 'P':
				b.WriteRune(0x2029)
			case 'x', 'u', 'U':
				count := 2
				switch c {
				case 'u':
					count = 4
				case 'U':
					count = 8
				}
				if f.pos+count > f.limit {
					return nil, errAt(f.pos, "unfinished Unicode escape")
				}
				v, e := strconv.ParseUint(string(s[f.pos:f.pos+count]), 16, 32)
				if e != nil || v > utf8.MaxRune || v >= 0xd800 && v <= 0xdfff {
					return nil, errAt(f.pos, "invalid Unicode escape")
				}
				b.WriteRune(rune(v))
				f.pos += count
			default:
				return nil, errAt(f.pos-1, "unknown escape")
			}
			contentEnd = b.Len()
			continue
		}
		b.WriteByte(c)
		if c != ' ' && c != '\t' {
			contentEnd = b.Len()
		}
	}
	return nil, errAt(start, "unterminated quoted string")
}
func (f *flow) documentMarker() bool {
	s := f.p.src
	if f.pos > 0 && s[f.pos-1] != '\n' && s[f.pos-1] != '\r' {
		return false
	}
	if f.pos+3 > f.limit {
		return false
	}
	token := string(s[f.pos : f.pos+3])
	return (token == "---" || token == "...") && (f.pos+3 == f.limit || strings.ContainsRune(" \t\r\n", rune(s[f.pos+3])))
}

func (f *flow) foldBreak() int {
	count := 0
	s := f.p.src
	for f.pos < f.limit && (s[f.pos] == '\n' || s[f.pos] == '\r') {
		if s[f.pos] == '\r' {
			f.pos++
			if f.pos < f.limit && s[f.pos] == '\n' {
				f.pos++
			}
		} else {
			f.pos++
		}
		count++
		for f.pos < f.limit && (s[f.pos] == ' ' || s[f.pos] == '\t') {
			f.pos++
		}
	}
	return count
}
func (f *flow) continuationIndent() bool {
	start := f.pos
	for start > 0 && f.p.src[start-1] != '\n' && f.p.src[start-1] != '\r' {
		start--
	}
	i := start
	for i < f.pos && f.p.src[i] == ' ' {
		i++
	}
	return i-start >= f.minIndent
}
func (f *flow) collection() (*syntax.Node, error) {
	propertyDepth := f.propertyDepth
	f.propertyDepth = 0
	defer func() { f.propertyDepth = propertyDepth }()
	f.depth++
	defer func() { f.depth-- }()
	if f.depth > 256 {
		return nil, errAt(f.pos, "nesting limit exceeded")
	}
	start, open := f.pos, f.p.src[f.pos]
	close, kind := byte(']'), syntax.Array
	if open == '{' {
		close, kind = '}', syntax.Object
	}
	n := &syntax.Node{Kind: kind, Start: start, Style: "flow"}
	f.pos++
	for {
		if err := f.ws(); err != nil {
			return nil, err
		}
		if f.pos >= f.limit {
			return nil, errAt(start, "unterminated flow collection")
		}
		if f.p.src[f.pos] == close {
			f.pos++
			n.End = f.pos
			return n, nil
		}
		elemStart := f.pos
		explicit := f.explicitKey()
		if explicit {
			f.pos++
			if err := f.ws(); err != nil {
				return nil, err
			}
		}
		var key *syntax.Node
		var err error
		if f.pos < f.limit && (f.p.src[f.pos] == ':' && (f.pos+1 == f.limit || strings.ContainsRune(" \t\r\n,[]{}", rune(f.p.src[f.pos+1]))) || explicit && (f.p.src[f.pos] == ',' || f.p.src[f.pos] == close)) {
			key = emptyScalar(f.pos)
		} else {
			key, err = f.node(true)
			if err != nil {
				return nil, err
			}
		}
		if err := f.ws(); err != nil {
			return nil, err
		}
		hasColon := f.pos < f.limit && f.p.src[f.pos] == ':'
		if kind == syntax.Object || explicit || hasColon {
			if kind == syntax.Array && !explicit && bytes.ContainsAny(f.p.src[key.Start:f.pos], "\r\n") {
				return nil, errAt(key.Start, "implicit flow-pair keys must be on one line")
			}
			if kind == syntax.Array {
				f.depth++
				if f.depth > 256 {
					return nil, errAt(elemStart, "nesting limit exceeded")
				}
			}
			entry, err := f.mappingEntry(key, elemStart, close)
			if kind == syntax.Array {
				f.depth--
			}
			if err != nil {
				return nil, err
			}
			if kind == syntax.Object {
				n.Entries = append(n.Entries, entry)
			} else {
				n.Items = append(n.Items, &syntax.Node{Kind: syntax.Object, Start: elemStart, End: entry.End, Style: "flow-pair", Entries: []*syntax.Entry{entry}})
			}
		} else {
			if key.Start == key.End && elemStart == f.pos {
				return nil, errAt(f.pos, "missing sequence value")
			}
			n.Items = append(n.Items, key)
		}
		if err := f.ws(); err != nil {
			return nil, err
		}
		if f.pos >= f.limit {
			return nil, errAt(start, "unterminated flow collection")
		}
		if f.p.src[f.pos] == close {
			continue
		}
		if f.p.src[f.pos] != ',' {
			return nil, errAt(f.pos, "expected ','")
		}
		f.pos++
	}
}
func (f *flow) explicitKey() bool {
	return f.pos < f.limit && f.p.src[f.pos] == '?' && (f.pos+1 == f.limit || strings.ContainsRune(" \t\r\n", rune(f.p.src[f.pos+1])))
}
func (f *flow) mappingEntry(key *syntax.Node, start int, close byte) (*syntax.Entry, error) {
	hasColon := f.pos < f.limit && f.p.src[f.pos] == ':'
	if !hasColon && (f.pos >= f.limit || f.p.src[f.pos] != ',' && f.p.src[f.pos] != close) {
		return nil, errAt(f.pos, "expected ':'")
	}
	var value *syntax.Node
	if hasColon {
		f.pos++
		if err := f.ws(); err != nil {
			return nil, err
		}
		if f.pos < f.limit && (f.p.src[f.pos] == ',' || f.p.src[f.pos] == close) {
			value = emptyScalar(f.pos)
		} else {
			var err error
			value, err = f.node(true)
			if err != nil {
				return nil, err
			}
		}
	} else {
		value = emptyScalar(key.End)
		value.Meta["flowMissingColon"] = true
	}
	return &syntax.Entry{Key: keySpelling(key), KeyStart: key.Start, KeyEnd: key.End, Start: start, End: value.End, Value: value, Meta: map[string]any{"keyNode": key}}, nil
}
