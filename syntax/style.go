package syntax

import (
	"fmt"
	"slices"
	"strings"
)

// Style controls the spelling of newly generated values, not untouched source.
// Empty fields mean auto. Syntax constraints always take precedence over style.
// Objects: auto, block, flow, dotted (TOML only).
// Arrays: auto, block, flow. Block uses TOML array tables for nonempty object
// arrays and multiline brackets for other arrays.
// Strings: auto, plain (YAML), single, double, literal, folded (YAML),
// multiline (TOML basic strings). Keys: auto, plain, single, double.
// Numbers: auto, decimal, hex, octal, binary (TOML only). Non-integral and
// negative base-prefixed values retain a safe decimal representation.
// Indent is an indentation unit (spaces, or tabs for TOML); empty infers it.
// LineEnding: auto, lf, crlf.
type Style struct {
	Objects    string
	Arrays     string
	Strings    string
	Keys       string
	Numbers    string
	Indent     string
	LineEnding string
}

// IsAuto reports whether every field requests inference.
func (s Style) IsAuto() bool {
	return auto(s.Objects) && auto(s.Arrays) && auto(s.Strings) && auto(s.Keys) && auto(s.Numbers) && s.Indent == "" && auto(s.LineEnding)
}
func auto(s string) bool { return s == "" || s == "auto" }

// Validate checks a style against a built-in format. No filesystem is consulted.
func (s Style) Validate(format string) error {
	check := func(name, value, allowed string) error {
		if auto(value) || slices.Contains(strings.Split(allowed, "|"), value) {
			return nil
		}
		return fmt.Errorf("%s: unsupported %s style %q (expected auto, %s)", format, name, value, strings.ReplaceAll(allowed, "|", ", "))
	}
	objects, str, numbers := "block|flow", "single|double|literal", "decimal|hex|octal"
	switch format {
	case "toml":
		objects += "|dotted"
		str += "|multiline"
		numbers += "|binary"
	case "yaml", "yml":
		str += "|plain|folded"
	default:
		if !s.IsAuto() {
			return fmt.Errorf("%s: explicit styles are not supported", format)
		}
		return nil
	}
	for _, field := range [][3]string{{"object", s.Objects, objects}, {"array", s.Arrays, "block|flow"}, {"string", s.Strings, str}, {"key", s.Keys, "plain|single|double"}, {"number", s.Numbers, numbers}, {"line ending", s.LineEnding, "lf|crlf"}} {
		if err := check(field[0], field[1], field[2]); err != nil {
			return err
		}
	}
	if strings.Trim(s.Indent, " \t") != "" || len(s.Indent) > 16 || (format == "yaml" || format == "yml") && strings.Contains(s.Indent, "\t") {
		return fmt.Errorf("%s: indent must contain 1–16 spaces (TOML also allows tabs)", format)
	}
	return nil
}

// StyledFormat is an optional extension; existing Format implementations need
// not implement it. WithStyle must return an independent, immutable backend.
type StyledFormat interface{ WithStyle(Style) (Format, error) }

// NearbyTemplate chooses the majority representation among same-kind children
// in the nearest scope with evidence, breaking ties by the last source example.
// accept can distinguish arrays of objects from arrays of scalars. signature
// groups representations without treating values or comments as style.
func NearbyTemplate(parent *Node, kind Kind, accept func(*Node) bool, signature func(*Node) string) *Node {
	for scope := parent; scope != nil; scope = scope.Parent {
		counts := map[string]int{}
		var candidates []*Node
		add := func(v *Node) {
			if v != parent && v.Kind == kind && (accept == nil || accept(v)) {
				counts[signature(v)]++
				candidates = append(candidates, v)
			}
		}
		for _, e := range scope.Entries {
			add(e.Value)
		}
		for _, v := range scope.Items {
			add(v)
		}
		var best *Node
		count := 0
		for _, v := range candidates {
			c := counts[signature(v)]
			if c >= count {
				best = v
				count = c
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}
