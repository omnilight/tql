package yaml

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vasileknik76/tql/syntax"
)

type flow struct {
	p                 *parser
	pos, limit, depth int
	minIndent         int
}

func (p *parser) inline(start, end int, inFlow bool, indent int) (*syntax.Node, error) {
	f := &flow{p: p, pos: start, limit: end, minIndent: indent + 1}
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
	if rest < len(p.src) && p.src[rest] != '\r' && p.src[rest] != '\n' && p.src[rest] != '#' {
		return nil, errAt(rest, "unexpected scalar content")
	}
	return n, nil
}
func (f *flow) ws() {
	for f.pos < f.limit {
		c := f.p.src[f.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			f.pos++
			continue
		}
		if c == '#' {
			for f.pos < f.limit && f.p.src[f.pos] != '\n' && f.p.src[f.pos] != '\r' {
				f.pos++
			}
			continue
		}
		break
	}
}
func (f *flow) node(inFlow bool) (*syntax.Node, error) {
	if f.depth > 256 {
		return nil, errAt(f.pos, "nesting limit exceeded")
	}
	f.depth++
	defer func() { f.depth-- }()
	if inFlow {
		f.ws()
	}
	start := f.pos
	if start >= f.limit {
		return nil, errAt(start, "expected a value")
	}
	s := f.p.src
	if s[start] == '-' && (start+1 == f.limit || s[start+1] == ' ' || s[start+1] == '\t') {
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
		f.pos = next
		f.ws()
		n, e := f.propertyValue(inFlow)
		if e != nil {
			return nil, e
		}
		if e = f.p.bind(n, a); e != nil {
			return nil, e
		}
		return n, nil
	case '!':
		tag, next, e := tagProperty(s, start, f.limit)
		if e != nil {
			return nil, e
		}
		f.pos = next
		f.ws()
		oldForce := f.p.forceString
		if tag.name == "str" {
			f.p.forceString = true
		}
		n, e := f.propertyValue(inFlow)
		f.p.forceString = oldForce
		if e != nil {
			return nil, e
		}
		if e = f.p.applyTag(n, tag); e != nil {
			return nil, e
		}
		return n, nil
	case '?', '@', '`', '%', ']', '}', ',', '|', '>':
		return nil, errAt(start, "unsupported scalar indicator")
	case '[', '{':
		return f.collection()
	case '\'', '"':
		return f.quoted()
	}
	for f.pos < f.limit {
		c := s[f.pos]
		if c == '\n' || c == '\r' || inFlow && (c == ',' || c == ']' || c == '}') {
			break
		}
		if c == '#' && (f.pos == start || s[f.pos-1] == ' ' || s[f.pos-1] == '\t') {
			break
		}
		if c == ':' && (f.pos+1 == f.limit || strings.ContainsRune(" \t\r\n,[]{}", rune(s[f.pos+1]))) {
			break
		}
		if inFlow && (c == '[' || c == '{') {
			return nil, errAt(f.pos, "flow indicators are not allowed in a plain scalar")
		}
		f.pos++
	}
	end := f.pos
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	if end == start {
		return &syntax.Node{Kind: syntax.Null, Start: start, End: start}, nil
	}
	if f.p.forceString {
		return &syntax.Node{Kind: syntax.String, Value: string(s[start:end]), Start: start, End: end, Style: "plain"}, nil
	}
	return scalar(string(s[start:end]), start, end)
}
func (f *flow) propertyValue(inFlow bool) (*syntax.Node, error) {
	if f.pos >= f.limit || inFlow && strings.ContainsRune(",]}", rune(f.p.src[f.pos])) {
		return &syntax.Node{Kind: syntax.Null, Start: f.pos, End: f.pos}, nil
	}
	return f.node(inFlow)
}
func (f *flow) quoted() (*syntax.Node, error) {
	start := f.pos
	s := f.p.src
	q := s[f.pos]
	f.pos++
	var b strings.Builder
	for f.pos < f.limit {
		c := s[f.pos]
		f.pos++
		if c == q {
			if q == '\'' && f.pos < f.limit && s[f.pos] == '\'' {
				b.WriteByte('\'')
				f.pos++
				continue
			}
			return &syntax.Node{Kind: syntax.String, Value: b.String(), Start: start, End: f.pos, Style: map[byte]string{'\'': "single", '"': "double"}[q]}, nil
		}
		if c == '\n' || c == '\r' {
			f.pos--
			count := f.foldBreak()
			if !f.continuationIndent() {
				return nil, errAt(f.pos, "insufficient indentation in quoted scalar")
			}
			old := strings.TrimRight(b.String(), " \t")
			b.Reset()
			b.WriteString(old)
			if count == 1 {
				b.WriteByte(' ')
			} else {
				b.WriteString(strings.Repeat("\n", count-1))
			}
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
			continue
		}
		b.WriteByte(c)
	}
	return nil, errAt(start, "unterminated quoted string")
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
	start := f.pos
	s := f.p.src
	open := s[f.pos]
	close := byte(']')
	kind := syntax.Array
	if open == '{' {
		close = '}'
		kind = syntax.Object
	}
	n := &syntax.Node{Kind: kind, Start: start, Style: "flow"}
	f.pos++
	seen := map[string]bool{}
	for {
		f.ws()
		if f.pos >= f.limit {
			return nil, errAt(start, "unterminated flow collection")
		}
		if s[f.pos] == close {
			f.pos++
			n.End = f.pos
			return n, nil
		}
		elemStart := f.pos
		var v *syntax.Node
		var e error
		if kind == syntax.Object {
			var key string
			ks := f.pos
			var ke int
			if s[f.pos] == '\'' || s[f.pos] == '"' {
				kn, er := f.quoted()
				if er != nil {
					return nil, er
				}
				key = kn.Value.(string)
				ke = kn.End
				f.ws()
			} else {
				for f.pos < f.limit {
					c := s[f.pos]
					if c == ':' && (f.pos+1 == f.limit || strings.ContainsRune(" \t\r\n[]{}\"'", rune(s[f.pos+1]))) {
						break
					}
					if c == ',' || c == '}' || c == '\n' || c == '\r' {
						return nil, errAt(f.pos, "expected ':' in flow mapping")
					}
					f.pos++
				}
				ke = f.pos
				for ke > ks && (s[ke-1] == ' ' || s[ke-1] == '\t') {
					ke--
				}
				key = string(s[ks:ke])
				if key == "" || strings.ContainsAny(key, "[]{}&*!?") {
					return nil, errAt(ks, "unsupported mapping key")
				}
			}
			if f.pos >= f.limit || s[f.pos] != ':' {
				return nil, errAt(f.pos, "expected ':'")
			}
			if seen[key] {
				return nil, errAt(ks, "duplicate mapping key")
			}
			if key == "<<" {
				return nil, errAt(ks, "merge keys are unsupported")
			}
			seen[key] = true
			f.pos++
			f.ws()
			if f.pos < f.limit && (s[f.pos] == ',' || s[f.pos] == '}') {
				v = &syntax.Node{Kind: syntax.Null, Start: f.pos, End: f.pos}
			} else {
				v, e = f.node(true)
			}
			if e != nil {
				return nil, e
			}
			n.Entries = append(n.Entries, &syntax.Entry{Key: key, KeyStart: ks, KeyEnd: ke, Start: elemStart, End: v.End, Value: v})
		} else {
			v, e = f.node(true)
			if e != nil {
				return nil, e
			}
			if v.Start == v.End && elemStart == f.pos {
				return nil, errAt(f.pos, "missing sequence value")
			}
			n.Items = append(n.Items, v)
		}
		f.ws()
		if f.pos >= f.limit {
			return nil, errAt(start, "unterminated flow collection")
		}
		if s[f.pos] == close {
			continue
		}
		if s[f.pos] != ',' {
			return nil, errAt(f.pos, "expected ','")
		}
		f.pos++
	}
}
