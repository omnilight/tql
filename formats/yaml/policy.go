package yaml

import (
	"strconv"
	"strings"
)

func (s insertionStyle) overrides() insertionStyle {
	if s.policy.Keys != "" && s.policy.Keys != "auto" {
		s.key = s.policy.Keys
	}
	if s.policy.Strings != "" && s.policy.Strings != "auto" {
		s.str = s.policy.Strings
	}
	if s.policy.Indent != "" {
		s.step = len(s.policy.Indent)
	}
	switch s.policy.LineEnding {
	case "lf":
		s.nl = "\n"
	case "crlf":
		s.nl = "\r\n"
	}
	return s
}

// A probe ensures chomping, folding and leading whitespace preserve the exact
// string. Values unsuitable for block scalars use escaped double quotes.
func (s insertionStyle) blockString(value, style string, indent int) (string, bool) {
	if strings.Contains(value, "\r") || strings.IndexFunc(value, func(r rune) bool {
		return r < 0x20 && r != '\n' && r != '\t' || r == 0x85 || r == 0x2028 || r == 0x2029
	}) >= 0 {
		return "", false
	}
	step := s.step
	indicator := ""
	if step <= 9 {
		indicator = strconv.Itoa(step)
	}
	pad := strings.Repeat(" ", step)
	chomp := "-"
	if strings.HasSuffix(value, "\n") {
		chomp = ""
		if strings.HasSuffix(value, "\n\n") {
			chomp = "+"
		}
	}
	marker := "|"
	body := value
	if style == "folded" {
		marker = ">"
		body = strings.ReplaceAll(body, "\n", "\n\n")
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body = strings.TrimSuffix(body, "\n")
	header := marker + indicator + chomp
	probe := header + "\n" + pad + strings.ReplaceAll(body, "\n", "\n"+pad)
	n, err := (Format{}).Parse([]byte("x: " + probe + "\n"))
	if err != nil || n.Get("x") == nil || n.Get("x").Value != value {
		if style == "folded" {
			return s.blockString(value, "literal", indent)
		}
		return "", false
	}
	actualPad := strings.Repeat(" ", max(0, indent-s.step)+step)
	return header + s.nl + actualPad + strings.ReplaceAll(body, "\n", s.nl+actualPad), true
}
