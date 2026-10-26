package yaml

import (
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

type tagInfo struct {
	name       string
	start, end int
}

func tagProperty(src []byte, start, end int) (tagInfo, int, error) {
	pos := start
	if strings.HasPrefix(string(src[start:end]), "!<") {
		pos += 2
		for pos < end && src[pos] != '>' {
			pos++
		}
		if pos == end {
			return tagInfo{}, pos, errAt(start, "unterminated tag")
		}
		pos++
	} else {
		for pos < end && !strings.ContainsRune(" \t\r\n[]{},", rune(src[pos])) {
			pos++
		}
	}
	raw := string(src[start:pos])
	name := strings.TrimPrefix(raw, "!!")
	if strings.HasPrefix(raw, "!<tag:yaml.org,2002:") && strings.HasSuffix(raw, ">") {
		name = strings.TrimSuffix(strings.TrimPrefix(raw, "!<tag:yaml.org,2002:"), ">")
	} else if !strings.HasPrefix(raw, "!!") {
		return tagInfo{}, pos, errAt(start, "custom tags are unsupported")
	}
	switch name {
	case "str", "int", "float", "bool", "null", "seq", "map":
		return tagInfo{name, start, pos}, pos, nil
	}
	return tagInfo{}, pos, errAt(start, "unsupported native tag "+name)
}
func pendingTag(n *syntax.Node) (tagInfo, bool) {
	t, ok := n.Meta["pendingTag"].(tagInfo)
	return t, ok
}
func (p *parser) applyTag(n *syntax.Node, t tagInfo) error {
	if _, alias := n.Meta["aliasName"]; alias {
		return errAt(t.start, "alias nodes cannot have tag properties")
	}
	if _, exists := n.Meta["tag"]; exists {
		return errAt(t.start, "duplicate tag properties")
	}
	value := n.Value
	text := ""
	if n.Kind == syntax.String {
		text = value.(string)
	} else if n.Style == "plain" {
		text = string(p.src[n.Start:n.End])
	}
	switch t.name {
	case "str":
		if n.Kind == syntax.Object || n.Kind == syntax.Array {
			return errAt(t.start, "!!str requires a scalar")
		}
		if n.Kind == syntax.Null && n.Start == n.End {
			text = ""
		}
		n.Kind = syntax.String
		n.Value = text
	case "int", "float":
		resolved, e := scalar(text, n.Start, n.End)
		if e != nil || resolved.Kind != syntax.NumberKind {
			return errAt(t.start, "invalid !!"+t.name+" scalar")
		}
		if t.name == "int" && !intRE.MatchString(string(resolved.Value.(syntax.Number))) {
			return errAt(t.start, "invalid !!int scalar")
		}
		n.Kind = syntax.NumberKind
		n.Value = resolved.Value
	case "bool":
		resolved, e := scalar(text, n.Start, n.End)
		if e != nil || resolved.Kind != syntax.Bool {
			return errAt(t.start, "invalid !!bool scalar")
		}
		n.Kind = syntax.Bool
		n.Value = resolved.Value
	case "null":
		resolved, e := scalar(text, n.Start, n.End)
		if text == "" {
			resolved = &syntax.Node{Kind: syntax.Null}
		}
		if e != nil || resolved.Kind != syntax.Null {
			return errAt(t.start, "invalid !!null scalar")
		}
		n.Kind = syntax.Null
		n.Value = nil
	case "map":
		if n.Kind != syntax.Object {
			return errAt(t.start, "!!map requires a mapping")
		}
	case "seq":
		if n.Kind != syntax.Array {
			return errAt(t.start, "!!seq requires a sequence")
		}
	}
	n.Meta = meta(n.Meta)
	n.Meta["tag"] = t
	return nil
}
func tagFits(t tagInfo, value any) bool {
	switch t.name {
	case "str":
		switch value.(type) {
		case string, syntax.Temporal:
			return true
		}
	case "int":
		switch x := value.(type) {
		case syntax.Number:
			return intRE.MatchString(string(x))
		case int, int64:
			return true
		}
	case "float":
		switch value.(type) {
		case syntax.Number, int, int64, float64:
			return true
		}
	case "bool":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "map":
		_, ok := value.(map[string]any)
		return ok
	case "seq":
		_, ok := value.([]any)
		return ok
	}
	return false
}
func tagEdits(target *syntax.Node, value any, edits []syntax.Edit) []syntax.Edit {
	if tag, ok := target.Meta["tag"].(tagInfo); ok && !tagFits(tag, value) {
		edits = append(edits, syntax.Edit{Start: tag.start, End: tag.end})
	}
	return edits
}
