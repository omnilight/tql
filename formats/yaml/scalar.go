package yaml

import (
	"math/big"
	"regexp"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

var intRE = regexp.MustCompile(`^[+-]?[0-9]+$`)
var floatRE = regexp.MustCompile(`^[+-]?([0-9]+\.[0-9]*|\.[0-9]+|[0-9]+)([eE][+-]?[0-9]+)?$`)

func scalar(s string, start, end int) (*syntax.Node, error) {
	n := &syntax.Node{Kind: syntax.String, Value: s, Start: start, End: end, Style: "plain"}
	switch s {
	case "~", "null", "Null", "NULL":
		n.Kind = syntax.Null
		n.Value = nil
	case "true", "True", "TRUE":
		n.Kind = syntax.Bool
		n.Value = true
	case "false", "False", "FALSE":
		n.Kind = syntax.Bool
		n.Value = false
	default:
		if s == ".nan" || s == ".NaN" || s == ".NAN" || strings.EqualFold(strings.TrimPrefix(strings.TrimPrefix(s, "+"), "-"), ".inf") {
			return nil, errAt(start, "non-finite numbers cannot be represented by TQL")
		}
		sign := ""
		unsigned := s
		if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
			sign = s[:1]
			unsigned = s[1:]
		}
		if strings.HasPrefix(unsigned, "0x") || strings.HasPrefix(unsigned, "0o") {
			base := 16
			if unsigned[1] == 'o' {
				base = 8
			}
			digits := unsigned[2:]
			allowed := "01234567"
			if base == 16 {
				allowed = "0123456789abcdefABCDEF"
			}
			if digits != "" && strings.Trim(digits, allowed) == "" {
				z, _ := new(big.Int).SetString(digits, base)
				if sign == "-" {
					z.Neg(z)
				}
				n.Kind = syntax.NumberKind
				n.Value = syntax.Number(z.String())
			}
		} else if intRE.MatchString(s) {
			z, _ := new(big.Int).SetString(s, 10)
			n.Kind = syntax.NumberKind
			n.Value = syntax.Number(z.String())
		} else if floatRE.MatchString(s) {
			x := strings.TrimPrefix(s, "+")
			if strings.HasPrefix(x, ".") {
				x = "0" + x
			}
			if strings.HasPrefix(x, "-.") {
				x = "-0" + x[1:]
			}
			idx := strings.IndexAny(x, "eE")
			mant, exp := x, ""
			if idx >= 0 {
				mant, exp = x[:idx], x[idx:]
			}
			if strings.HasSuffix(mant, ".") {
				mant += "0"
			}
			negative := strings.HasPrefix(mant, "-")
			unsignedMant := strings.TrimPrefix(mant, "-")
			parts := strings.SplitN(unsignedMant, ".", 2)
			parts[0] = strings.TrimLeft(parts[0], "0")
			if parts[0] == "" {
				parts[0] = "0"
			}
			mant = strings.Join(parts, ".")
			if negative {
				mant = "-" + mant
			}
			n.Kind = syntax.NumberKind
			n.Value = syntax.Number(mant + exp)
		}
	}
	return n, nil
}
func (p *parser) blockScalar(start, end, indent int) (*syntax.Node, error) {
	head := string(p.src[start:end])
	if i := strings.IndexByte(head, '#'); i >= 0 {
		if i > 0 && head[i-1] != ' ' && head[i-1] != '\t' {
			return nil, errAt(start+i, "comment must be separated")
		}
		head = head[:i]
	}
	head = strings.TrimSpace(head)
	style := head[0]
	chomp := byte(0)
	explicit := 0
	for i := 1; i < len(head); i++ {
		c := head[i]
		if c == '+' || c == '-' {
			if chomp != 0 {
				return nil, errAt(start, "duplicate chomping indicator")
			}
			chomp = c
		} else if c >= '1' && c <= '9' {
			if explicit != 0 {
				return nil, errAt(start, "duplicate indentation indicator")
			}
			explicit = int(c - '0')
		} else {
			return nil, errAt(start+i, "invalid block scalar header")
		}
	}
	contentIndent := indent + explicit
	j := p.i
	if explicit == 0 {
		for j < len(p.lines) && strings.TrimSpace(p.lines[j].text) == "" {
			j++
		}
		if j < len(p.lines) && p.lines[j].indent > indent {
			contentIndent = p.lines[j].indent
		} else {
			contentIndent = indent + 1
			// With no nonempty content line, whitespace-only lines establish
			// indentation themselves instead of exceeding an inferred width.
			for k := p.i; k < j; k++ {
				if p.lines[k].indent > contentIndent {
					contentIndent = p.lines[k].indent
				}
			}
		}
	}
	var rows []string
	var terminated []bool
	contentTerminated := false
	last := end
	leading := true
	for p.i < len(p.lines) {
		l := p.lines[p.i]
		blank := strings.TrimSpace(l.text) == ""
		if explicit == 0 && leading && blank && l.indent > contentIndent {
			return nil, errAt(l.start, "leading empty block scalar line is over-indented")
		}
		if !blank {
			leading = false
		}
		if !blank && l.indent < contentIndent {
			if l.indent > indent {
				return nil, errAt(l.start, "invalid block scalar indentation")
			}
			break
		}
		text := ""
		if l.end-l.start >= contentIndent {
			text = string(p.src[l.start+contentIndent : l.end])
		}
		rows = append(rows, text)
		terminated = append(terminated, l.next > l.end)
		if text != "" {
			contentTerminated = l.next > l.end
		}
		last = l.next
		p.i++
	}
	var b strings.Builder
	for i, s := range rows {
		b.WriteString(s)
		if i+1 < len(rows) {
			if style == '>' && s != "" && rows[i+1] != "" && !strings.HasPrefix(s, " ") && !strings.HasPrefix(rows[i+1], " ") {
				b.WriteByte(' ')
			} else if style == '>' && s != "" && rows[i+1] == "" && !strings.HasPrefix(s, " ") {
				j := i + 1
				for j < len(rows) && rows[j] == "" {
					j++
				}
				if j == len(rows) || strings.HasPrefix(rows[j], " ") {
					b.WriteByte('\n')
				}
			} else {
				b.WriteByte('\n')
			}
		} else if terminated[i] {
			b.WriteByte('\n')
		}
	}
	value := b.String()
	if chomp == '-' {
		value = strings.TrimRight(value, "\n")
	} else if chomp != '+' {
		value = strings.TrimRight(value, "\n")
		if value != "" && contentTerminated {
			value += "\n"
		}
	}
	return &syntax.Node{Kind: syntax.String, Value: value, Start: start, End: last, Style: "block-scalar", Meta: map[string]any{"indent": indent, "headerEnd": end}}, nil
}
