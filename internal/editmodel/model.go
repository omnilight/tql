// Package editmodel holds the immutable data shared by syntax and the edit engine.
package editmodel

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Step addresses an object key or array index.
type Step struct {
	Key   string
	Index int
	Array bool
}

// Path addresses one location. Its zero value is the document root.
type Path struct {
	steps []Step
	err   error
}

// Key returns a new path with literal object keys appended.
func (p Path) Key(keys ...string) Path {
	steps := make([]Step, len(p.steps), len(p.steps)+len(keys))
	copy(steps, p.steps)
	err := p.err
	for _, key := range keys {
		if !utf8.ValidString(key) && err == nil {
			err = fmt.Errorf("invalid UTF-8 path key")
		}
		steps = append(steps, Step{Key: key})
	}
	return Path{steps: steps, err: err}
}

// Index returns a new path with a nonnegative array index appended.
// A negative index is reported when the path is used in a command or merge option.
func (p Path) Index(index int) Path {
	steps := make([]Step, len(p.steps)+1)
	copy(steps, p.steps)
	steps[len(p.steps)] = Step{Index: index, Array: true}
	err := p.err
	if index < 0 && err == nil {
		err = fmt.Errorf("negative array index %d", index)
	}
	return Path{steps: steps, err: err}
}

// String returns a canonical TQL address with quoted literal keys.
func (p Path) String() string {
	var b strings.Builder
	b.WriteByte('.')
	for _, s := range p.steps {
		b.WriteByte('[')
		if s.Array {
			b.WriteString(strconv.Itoa(s.Index))
		} else {
			b.WriteString(quote(s.Key))
		}
		b.WriteByte(']')
	}
	return b.String()
}

// quote uses JSON escaping without introducing a dependency on syntax.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Steps returns the internal path steps. Callers must not modify them.
func Steps(p Path) []Step { return p.steps }

// PathError returns a path's construction error, if any.
func PathError(p Path) error { return p.err }

// Kind identifies a structured edit command.
type Kind string

const (
	// SetOperation assigns a value.
	SetOperation Kind = "set"
	// DelOperation removes a field or array element.
	DelOperation Kind = "del"
)

// Operation is an immutable structured command. Its zero value is invalid.
type Operation struct {
	kind  Kind
	path  Path
	value any
	err   error
}

// New creates a command from an already normalized, detached value.
func New(kind Kind, path Path, value any, err error) Operation {
	return Operation{kind: kind, path: path, value: value, err: err}
}

// Kind returns the command kind.
func (op Operation) Kind() Kind { return op.kind }

// Path returns the command's immutable address.
func (op Operation) Path() Path { return op.path }

// Value returns a detached assigned value, or nil for deletion.
func (op Operation) Value() any { return clone(op.value) }

// Validate checks command construction, paths, and kind.
func Validate(op Operation) error {
	if op.err != nil {
		return op.err
	}
	if op.path.err != nil {
		return op.path.err
	}
	if op.kind != SetOperation && op.kind != DelOperation {
		return fmt.Errorf("invalid operation kind %q", op.kind)
	}
	if op.kind == DelOperation && len(op.path.steps) == 0 {
		return fmt.Errorf("cannot delete document root")
	}
	return nil
}

func clone(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = clone(v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = clone(v)
		}
		return out
	default:
		return v
	}
}
