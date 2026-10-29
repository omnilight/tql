package toml

import (
	"fmt"
	"sort"
	"strings"

	scalarstyle "github.com/vasileknik76/tql/internal/style"
	"github.com/vasileknik76/tql/syntax"
)

func (s insertionStyle) overrides() insertionStyle {
	switch s.policy.Keys {
	case "plain":
		s.keyQuote = 0
	case "single":
		s.keyQuote = '\''
	case "double":
		s.keyQuote = '"'
	}
	switch s.policy.Strings {
	case "single":
		s.stringQuote = '\''
	case "double":
		s.stringQuote = '"'
	}
	if s.policy.Indent != "" {
		s.unit = s.policy.Indent
	}
	switch s.policy.LineEnding {
	case "lf":
		s.nl = "\n"
	case "crlf":
		s.nl = "\r\n"
	}
	return s
}
func tableArray(xs []any) bool { return scalarstyle.ObjectArray(xs) }

func objectMode(policy syntax.Style, t *syntax.Node) string {
	if policy.Objects != "" && policy.Objects != "auto" {
		return policy.Objects
	}
	if t != nil && t.Kind == syntax.Object {
		switch t.Style {
		case "inline":
			return "flow"
		case "dotted":
			return "dotted"
		}
		if t.Meta["dotted"] == true {
			return "dotted"
		}
		return "block"
	}
	return "block"
}
func arrayTables(policy syntax.Style, t *syntax.Node, objects string) bool {
	if policy.Arrays == "flow" {
		return false
	}
	if policy.Arrays == "block" {
		return true
	}
	if t != nil && t.Kind == syntax.Array {
		return t.Style == "tables"
	}
	return objects == "block"
}
func collectionTemplate(src []byte, parent *syntax.Node, value any) *syntax.Node {
	if parent == nil {
		return nil
	}
	kind := valueKind(value)
	accept := func(n *syntax.Node) bool {
		if n == parent {
			return false
		}
		if kind != syntax.Array {
			return true
		}
		return scalarstyle.ObjectArrayNode(n) == tableArray(value.([]any))
	}
	signature := func(n *syntax.Node) string {
		if kind == syntax.Object {
			return objectMode(syntax.Style{}, n)
		}
		return scalarstyle.Signature(src, n)
	}
	for scope := parent; scope != nil; scope = scope.Parent {
		// Adjacent array-table items form the same local scope for layout.
		// Inspect their body values, rather than comparing scalar arrays with
		// the synthetic array of tables that contains them.
		view := &syntax.Node{Entries: scope.Entries, Items: scope.Items}
		if scope.Style == "tables" {
			view = &syntax.Node{Items: scope.Items}
			for _, item := range scope.Items {
				if item != parent {
					view.Entries = append(view.Entries, sectionEntries(item)...)
				}
			}
		}
		if template := syntax.NearbyTemplate(view, kind, accept, signature); template != nil {
			return template
		}
	}
	return nil
}
func renderString(value, style, template string, q byte) string {
	if style == "" || style == "auto" {
		switch {
		case strings.HasPrefix(template, "'''"):
			style = "literal"
		case strings.HasPrefix(template, `"""`):
			style = "multiline"
		default:
			return styledString(value, q)
		}
	}
	var candidate string
	switch style {
	case "single":
		return styledString(value, '\'')
	case "literal":
		candidate = "'''\n" + value + "'''"
	case "multiline":
		parts := strings.Split(value, "\n")
		for i, p := range parts {
			v := quoteString(p)
			parts[i] = v[1 : len(v)-1]
		}
		candidate = "\"\"\"\n" + strings.Join(parts, "\n") + "\"\"\""
	default:
		return quoteString(value)
	}
	// Triple delimiters, control characters and a leading newline can make a
	// preferred spelling unsuitable. Probe it before falling back to escaping.
	n, err := (Format{}).Parse([]byte("v=" + candidate))
	if err == nil && n.Get("v").Value == value {
		return candidate
	}
	return quoteString(value)
}
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// renderSection emits assignments before descendant headers, so an assignment
// never accidentally becomes owned by the last child section.
func renderSection(src []byte, m map[string]any, path []string, t *syntax.Node, s insertionStyle, depth int) (string, error) {
	if depth > 256 {
		return "", fmt.Errorf("TOML value nesting exceeds 256 levels")
	}
	if t != nil {
		inherited := localInsertionStyle(src, t)
		inherited.policy = s.policy
		s = inherited.overrides()
	}
	var body, children strings.Builder
	for _, k := range sortedKeys(m) {
		v := m[k]
		var ct *syntax.Node
		if t != nil {
			ct = t.Get(k)
			if ct == nil {
				ct = collectionTemplate(src, t, v)
			}
		}
		objectTemplate := ct
		if ct == nil || ct.Kind != syntax.Object {
			objectTemplate = collectionTemplate(src, t, map[string]any{})
		}
		mode := objectMode(s.policy, objectTemplate)
		next := append(append([]string(nil), path...), k)
		if obj, ok := v.(map[string]any); ok && mode == "block" {
			text, err := renderSection(src, obj, next, ct, s, depth+1)
			if err != nil {
				return "", err
			}
			children.WriteString(s.nl + "[" + styledPath(next, s) + "]" + s.nl + text)
			continue
		}
		if obj, ok := v.(map[string]any); ok && mode == "dotted" && len(obj) > 0 {
			text, headers, err := renderDotted(src, obj, []string{k}, path, ct, s, depth+1)
			if err != nil {
				return "", err
			}
			body.WriteString(text)
			children.WriteString(headers)
			continue
		}
		if xs, ok := v.([]any); ok && tableArray(xs) && arrayTables(s.policy, ct, mode) {
			for _, item := range xs {
				var exemplar *syntax.Node
				if ct != nil && len(ct.Items) > 0 {
					exemplar = ct.Items[len(ct.Items)-1]
				}
				text, err := renderSection(src, item.(map[string]any), next, exemplar, s, depth+1)
				if err != nil {
					return "", err
				}
				children.WriteString(s.nl + "[[" + styledPath(next, s) + "]]" + s.nl + text)
			}
			continue
		}
		text, err := renderInsertion(src, v, ct, s.forAssignment(styledKey(k, s)), depth+1)
		if err != nil {
			return "", err
		}
		body.WriteString(s.indent + styledAssignment(styledKey(k, s), text, s) + s.nl)
	}
	return body.String() + children.String(), nil
}

// renderDotted keeps assignments and headers separate even when a dotted
// object contains an array of tables. Callers emit all assignments first.
func renderDotted(src []byte, m map[string]any, path, ownerPath []string, t *syntax.Node, s insertionStyle, depth int) (string, string, error) {
	if depth > 256 {
		return "", "", fmt.Errorf("TOML value nesting exceeds 256 levels")
	}
	var body, headers strings.Builder
	for _, k := range sortedKeys(m) {
		next := append(append([]string(nil), path...), k)
		v := m[k]
		var ct *syntax.Node
		if t != nil {
			ct = t.Get(k)
			if ct == nil {
				ct = collectionTemplate(src, t, v)
			}
		}
		if obj, ok := v.(map[string]any); ok && len(obj) > 0 {
			text, children, err := renderDotted(src, obj, next, ownerPath, ct, s, depth+1)
			if err != nil {
				return "", "", err
			}
			body.WriteString(text)
			headers.WriteString(children)
			continue
		}
		if xs, ok := v.([]any); ok && tableArray(xs) && arrayTables(s.policy, ct, "dotted") {
			fullPath := append(append([]string(nil), ownerPath...), next...)
			var exemplar *syntax.Node
			if ct != nil && len(ct.Items) > 0 {
				exemplar = ct.Items[len(ct.Items)-1]
			}
			for _, item := range xs {
				text, err := renderSection(src, item.(map[string]any), fullPath, exemplar, s, depth+1)
				if err != nil {
					return "", "", err
				}
				headers.WriteString(s.nl + "[[" + styledPath(fullPath, s) + "]]" + s.nl + text)
			}
			continue
		}
		text, err := renderInsertion(src, v, ct, s.forAssignment(styledPath(next, s)), depth+1)
		if err != nil {
			return "", "", err
		}
		body.WriteString(s.indent + styledAssignment(styledPath(next, s), text, s) + s.nl)
	}
	return body.String(), headers.String(), nil
}

// sectionBoundary stays inside the selected array-table element. Appending at
// EOF would otherwise attach a child to a later element of the same array.
func sectionBoundary(src []byte, parent *syntax.Node) int {
	pos := len(src)
	for n := parent; n != nil; n = n.Parent {
		if n.Parent != nil && n.Parent.Style == "tables" {
			i := n.Parent.Index(n)
			if i+1 < len(n.Parent.Items) {
				if p, ok := n.Parent.Items[i+1].Meta["headerStart"].(int); ok && p < pos {
					pos = p
				}
			}
		}
	}
	return pos
}
func insertStructure(src []byte, parent *syntax.Node, key string, value any, t *syntax.Node, s insertionStyle, mode string) ([]syntax.Edit, error) {
	owner, relative, err := context(parent)
	if err != nil {
		return nil, err
	}
	if mode == "dotted" {
		if m, ok := value.(map[string]any); ok && len(m) > 0 {
			ownerPath, err := absolutePath(owner)
			if err != nil {
				return nil, err
			}
			text, headers, err := renderDotted(src, m, append(relative, key), ownerPath, t, s, 0)
			text += headers
			if err != nil {
				return nil, err
			}
			pos := assignmentPosition(src, rootOf(parent), owner)
			prefix := ""
			if pos > 0 && src[pos-1] != '\n' {
				prefix = s.nl
			}
			if pos == len(src) && pos > 0 && src[pos-1] != '\n' {
				text = strings.TrimSuffix(text, s.nl)
			}
			return []syntax.Edit{edit(pos, pos, prefix+text)}, nil
		}
	}
	path, err := absolutePath(parent)
	if err != nil {
		return nil, err
	}
	path = append(path, key)
	var text string
	switch v := value.(type) {
	case map[string]any:
		body, e := renderSection(src, v, path, t, s, 0)
		if e != nil {
			return nil, e
		}
		text = "[" + styledPath(path, s) + "]" + s.nl + body
	case []any:
		for _, item := range v {
			var exemplar *syntax.Node
			if t != nil && len(t.Items) > 0 {
				exemplar = t.Items[len(t.Items)-1]
			}
			body, e := renderSection(src, item.(map[string]any), path, exemplar, s, 0)
			if e != nil {
				return nil, e
			}
			if text != "" {
				text += s.nl
			}
			text += "[[" + styledPath(path, s) + "]]" + s.nl + body
		}
	}
	pos := sectionBoundary(src, parent)
	prefix := ""
	if pos > 0 {
		if src[pos-1] != '\n' {
			prefix = s.nl
		}
		if pos < 2 || src[pos-2] != '\n' {
			prefix += s.nl
		}
	}
	if pos == len(src) && pos > 0 && src[pos-1] != '\n' {
		text = strings.TrimSuffix(text, s.nl)
	}
	return []syntax.Edit{edit(pos, pos, prefix+text)}, nil
}
func rootOf(n *syntax.Node) *syntax.Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}
