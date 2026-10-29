// Package json implements RFC 8259 JSON with byte-accurate source locations.
// Duplicate object keys and unpaired UTF-16 surrogates are rejected for
// unambiguous paths and valid Unicode strings.
package json

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/vasileknik76/tql/internal/scan"
	"github.com/vasileknik76/tql/syntax"
)

// Format parses and edits JSON documents without reformatting untouched source.
type Format struct{}

var _ syntax.Format = Format{}

type parser struct {
	source []byte
	pos    int
}

// Parse reads one JSON value, surrounded only by JSON whitespace. Numbers retain
// their exact decimal spelling, including exponent and negative-zero notation.
func (Format) Parse(source []byte) (*syntax.Node, error) {
	p := &parser{source: source}
	if !utf8.Valid(source) {
		return nil, p.fail("invalid UTF-8")
	}
	p.space()
	n, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.pos != len(source) {
		return nil, p.fail("unexpected content after JSON value")
	}
	syntax.Link(n)
	return n, nil
}

func (p *parser) fail(message string) error {
	line, column := 1, 1
	for _, b := range p.source[:p.pos] {
		if b == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return fmt.Errorf("json: %s at line %d, column %d (byte %d)", message, line, column, p.pos)
}

func (p *parser) space() {
	for p.pos < len(p.source) {
		switch p.source[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (*syntax.Node, error) {
	if depth > 1024 {
		return nil, p.fail("nesting exceeds 1024 levels")
	}
	if p.pos == len(p.source) {
		return nil, p.fail("expected a value")
	}
	start := p.pos
	n := &syntax.Node{Start: start}
	switch p.source[p.pos] {
	case '{':
		n.Kind = syntax.Object
		p.pos++
		p.space()
		seen := map[string]bool{}
		if p.take('}') {
			break
		}
		for {
			if p.pos == len(p.source) || p.source[p.pos] != '"' {
				return nil, p.fail("expected a quoted object key")
			}
			keyStart := p.pos
			key, err := p.string()
			if err != nil {
				return nil, err
			}
			keyEnd := p.pos
			if seen[key] {
				return nil, p.fail("duplicate object key " + strconv.Quote(key))
			}
			seen[key] = true
			p.space()
			if !p.take(':') {
				return nil, p.fail("expected ':' after object key")
			}
			p.space()
			child, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			n.Entries = append(n.Entries, &syntax.Entry{Key: key, KeyStart: keyStart, KeyEnd: keyEnd, Start: keyStart, End: p.pos, Value: child})
			p.space()
			if p.take('}') {
				break
			}
			if !p.take(',') {
				return nil, p.fail("expected ',' or '}'")
			}
			p.space()
		}
	case '[':
		n.Kind = syntax.Array
		p.pos++
		p.space()
		if p.take(']') {
			break
		}
		for {
			child, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			n.Items = append(n.Items, child)
			p.space()
			if p.take(']') {
				break
			}
			if !p.take(',') {
				return nil, p.fail("expected ',' or ']'")
			}
			p.space()
		}
	case '"':
		n.Kind = syntax.String
		s, err := p.string()
		if err != nil {
			return nil, err
		}
		n.Value = s
	case 't':
		if !p.literal("true") {
			return nil, p.fail("expected 'true'")
		}
		n.Kind = syntax.Bool
		n.Value = true
	case 'f':
		if !p.literal("false") {
			return nil, p.fail("expected 'false'")
		}
		n.Kind = syntax.Bool
		n.Value = false
	case 'n':
		if !p.literal("null") {
			return nil, p.fail("expected 'null'")
		}
		n.Kind = syntax.Null
	default:
		if p.source[p.pos] != '-' && !digit(p.source[p.pos]) {
			return nil, p.fail("expected a JSON value")
		}
		if err := p.number(); err != nil {
			return nil, err
		}
		n.Kind = syntax.NumberKind
		n.Value = syntax.Number(p.source[start:p.pos])
	}
	n.End = p.pos
	return n, nil
}

func (p *parser) take(b byte) bool {
	if p.pos < len(p.source) && p.source[p.pos] == b {
		p.pos++
		return true
	}
	return false
}
func (p *parser) literal(s string) bool {
	if bytes.HasPrefix(p.source[p.pos:], []byte(s)) {
		p.pos += len(s)
		return true
	}
	return false
}
func digit(b byte) bool { return b >= '0' && b <= '9' }
func (p *parser) number() error {
	p.take('-')
	if !p.take('0') {
		if p.pos == len(p.source) || p.source[p.pos] < '1' || p.source[p.pos] > '9' {
			return p.fail("expected integer part")
		}
		for p.pos < len(p.source) && digit(p.source[p.pos]) {
			p.pos++
		}
	}
	if p.take('.') {
		start := p.pos
		for p.pos < len(p.source) && digit(p.source[p.pos]) {
			p.pos++
		}
		if p.pos == start {
			return p.fail("expected fractional digits")
		}
	}
	if p.take('e') || p.take('E') {
		if !p.take('+') {
			p.take('-')
		}
		start := p.pos
		for p.pos < len(p.source) && digit(p.source[p.pos]) {
			p.pos++
		}
		if p.pos == start {
			return p.fail("expected exponent digits")
		}
	}
	return nil
}

func (p *parser) string() (string, error) {
	p.pos++ // opening quote was checked by the caller
	start := p.pos
	// Keep short keys in this function; vector setup costs more than their scan.
	p.pos += scan.JSONStringScalar(p.source[p.pos:min(p.pos+16, len(p.source))])
	if p.pos-start == 16 {
		p.pos += scan.JSONString(p.source[p.pos:])
	}
	if p.pos == len(p.source) {
		return "", p.fail("unterminated string")
	}
	if p.source[p.pos] == '"' {
		// Copy once. The tree must own its strings independently of the
		// caller's mutable source bytes.
		s := string(p.source[start:p.pos])
		p.pos++
		return s, nil
	}
	return p.escapedString(start)
}

func (p *parser) escapedString(start int) (string, error) {
	var b strings.Builder
	b.Write(p.source[start:p.pos])
	run := 0
	for p.pos < len(p.source) {
		c := p.source[p.pos]
		p.pos++
		switch {
		case c == '"':
			return b.String(), nil
		case c < 0x20:
			return "", p.fail("unescaped control character in string")
		case c != '\\':
			b.WriteByte(c)
			run++
			if run == 16 {
				end := p.pos + scan.JSONString(p.source[p.pos:])
				b.Write(p.source[p.pos:end])
				p.pos = end
				run = 0
			}
		default:
			run = 0
			if p.pos == len(p.source) {
				return "", p.fail("incomplete string escape")
			}
			e := p.source[p.pos]
			p.pos++
			switch e {
			case '"', '\\', '/':
				b.WriteByte(e)
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				r, err := p.hex4()
				if err != nil {
					return "", err
				}
				if r >= 0xd800 && r <= 0xdbff {
					if !p.take('\\') || !p.take('u') {
						return "", p.fail("high surrogate requires a low surrogate escape")
					}
					low, err := p.hex4()
					if err != nil {
						return "", err
					}
					if low < 0xdc00 || low > 0xdfff {
						return "", p.fail("invalid low surrogate")
					}
					r = utf16.DecodeRune(r, low)
				} else if r >= 0xdc00 && r <= 0xdfff {
					return "", p.fail("unpaired low surrogate")
				}
				b.WriteRune(r)
			default:
				return "", p.fail("invalid string escape")
			}
		}
	}
	return "", p.fail("unterminated string")
}

func (p *parser) hex4() (rune, error) {
	var n rune
	for range 4 {
		if p.pos == len(p.source) {
			return 0, p.fail("incomplete Unicode escape")
		}
		b := p.source[p.pos]
		p.pos++
		n <<= 4
		switch {
		case b >= '0' && b <= '9':
			n += rune(b - '0')
		case b >= 'a' && b <= 'f':
			n += rune(b - 'a' + 10)
		case b >= 'A' && b <= 'F':
			n += rune(b - 'A' + 10)
		default:
			return 0, p.fail("invalid Unicode escape")
		}
	}
	return n, nil
}
