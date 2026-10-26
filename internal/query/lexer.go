package query

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type token struct {
	kind, text string
	pos        int
}

func lex(s string) ([]token, error) {
	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("TQL: query is not valid UTF-8")
	}
	var ts []token
	fail := func(i int, msg string) ([]token, error) { return nil, fmt.Errorf("TQL at byte %d: %s", i+1, msg) }
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			i += n
			continue
		}
		if s[i] == '#' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		start := i
		if s[i] == '"' || s[i] == '\'' {
			quote := s[i]
			i++
			var b strings.Builder
			closed := false
			for i < len(s) {
				c := s[i]
				i++
				if c == quote {
					closed = true
					break
				}
				if c < 0x20 {
					return fail(i-1, "control character in string")
				}
				if c != '\\' {
					b.WriteByte(c)
					continue
				}
				if i >= len(s) {
					return fail(i, "unterminated escape")
				}
				c = s[i]
				i++
				switch c {
				case '"', '\\', '/':
					b.WriteByte(c)
				case '\'':
					if quote != '\'' {
						return fail(i-1, "invalid escape")
					}
					b.WriteByte(c)
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
					if i+4 > len(s) {
						return fail(i, "short Unicode escape")
					}
					u, err := strconv.ParseUint(s[i:i+4], 16, 16)
					if err != nil {
						return fail(i, "invalid Unicode escape")
					}
					i += 4
					ru := rune(u)
					if ru >= 0xD800 && ru <= 0xDBFF {
						if i+6 > len(s) || s[i:i+2] != `\u` {
							return fail(i, "missing low surrogate")
						}
						v, err := strconv.ParseUint(s[i+2:i+6], 16, 16)
						if err != nil || v < 0xDC00 || v > 0xDFFF {
							return fail(i, "invalid low surrogate")
						}
						i += 6
						ru = 0x10000 + (ru-0xD800)*0x400 + rune(v) - 0xDC00
					} else if ru >= 0xDC00 && ru <= 0xDFFF {
						return fail(i, "unpaired surrogate")
					}
					b.WriteRune(ru)
				default:
					return fail(i-1, "invalid escape")
				}
			}
			if !closed {
				return fail(start, "unterminated string")
			}
			ts = append(ts, token{"string", b.String(), start})
			continue
		}
		if s[i] >= '0' && s[i] <= '9' {
			i++
			if s[start] == '0' && i < len(s) && s[i] >= '0' && s[i] <= '9' {
				return fail(start, "leading zero in number")
			}
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			if i < len(s) && s[i] == '.' {
				i++
				j := i
				for i < len(s) && s[i] >= '0' && s[i] <= '9' {
					i++
				}
				if i == j {
					return fail(i, "expected fractional digits")
				}
			}
			if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
				i++
				if i < len(s) && (s[i] == '+' || s[i] == '-') {
					i++
				}
				j := i
				for i < len(s) && s[i] >= '0' && s[i] <= '9' {
					i++
				}
				if i == j {
					return fail(i, "expected exponent digits")
				}
			}
			ts = append(ts, token{"number", s[start:i], start})
			continue
		}
		if unicode.IsLetter(r) || r == '_' {
			i += n
			for i < len(s) {
				r, n = utf8.DecodeRuneInString(s[i:])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
					break
				}
				i += n
			}
			v := s[start:i]
			k := "ident"
			switch v {
			case "true", "false", "null", "and", "or", "not":
				k = v
			}
			ts = append(ts, token{k, v, start})
			continue
		}
		found := false
		for _, op := range []string{"??=", "|=", "+=", "-=", "*=", "/=", "==", "!=", "<=", ">=", "??", "&&", "||"} {
			if strings.HasPrefix(s[i:], op) {
				k := op
				if k == "&&" {
					k = "and"
				}
				if k == "||" {
					k = "or"
				}
				ts = append(ts, token{k, op, i})
				i += len(op)
				found = true
				break
			}
		}
		if found {
			continue
		}
		if strings.ContainsRune(".[]{}():,;=|+-*/%<>!$", r) {
			ts = append(ts, token{string(r), string(r), i})
			i += n
			continue
		}
		return fail(i, "unexpected character "+strconv.QuoteRune(r))
	}
	ts = append(ts, token{"eof", "", len(s)})
	return ts, nil
}

// dynamicSelector recognizes variable-derived index/key computations. Boolean
// comparisons and logical combinations remain element predicates even when
// their operands reference variables (for example [name == $name]).
