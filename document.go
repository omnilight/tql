package tql

import (
	"bytes"
	"errors"

	"github.com/vasileknik76/tql/syntax"
)

// Parse copies source and parses it using a registered format name.
func Parse(source []byte, format string, options ...Option) (*Document, error) {
	return defaultRegistry.Parse(source, format, options...)
}

// ParseWith uses an explicit backend without modifying a registry.
func ParseWith(source []byte, format Format, options ...Option) (*Document, error) {
	if format == nil {
		return nil, errors.New("nil format")
	}
	format, err := configuredFormat(format, options)
	if err != nil {
		return nil, err
	}
	src := bytes.Clone(source)
	root, err := format.Parse(src)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errors.New("format returned a nil tree")
	}
	syntax.Link(root)
	return &Document{source: src, root: root, format: format}, nil
}

// Document owns a parsed configuration and its original source. Create one with
// Parse or ParseWith; the zero value is not ready for use. A Document must not be
// read or modified concurrently without external synchronization.
type Document struct {
	source  []byte
	root    *syntax.Node
	format  Format
	runtime *scriptRuntime
}

// Bytes returns a copy of the current source.
func (d *Document) Bytes() []byte { return bytes.Clone(d.source) }

// Value returns a detached representation using maps, slices, and scalar values.
func (d *Document) Value() any { return d.root.Interface() }

// Result contains read outputs and the final source. Mutation statements have no
// read output. Bytes and Values are independent of the Document's internal data.
type Result struct {
	Values  []any
	Bytes   []byte
	Changed bool
}

// Exec compiles and executes a query or script. Any error leaves d unchanged.
func (d *Document) Exec(text string) (*Result, error) {
	p, err := Compile(text)
	if err != nil {
		return nil, err
	}
	return d.Execute(p)
}

// Query evaluates a read-only program and refuses mutation statements.
func (d *Document) Query(text string) ([]any, error) {
	p, err := Compile(text)
	if err != nil {
		return nil, err
	}
	if p.Mutates() {
		return nil, errors.New("query accepts read-only expressions; use Exec for mutations")
	}
	r, err := d.execute(p, false)
	if err != nil {
		return nil, err
	}
	return r.Values, nil
}

// Eval is the one-shot equivalent of Parse followed by Exec.
func Eval(source []byte, format, text string, options ...Option) (*Result, error) {
	d, err := Parse(source, format, options...)
	if err != nil {
		return nil, err
	}
	return d.Exec(text)
}

// Execute runs a compiled program and commits only if every statement succeeds.
// Program may be reused concurrently with other Documents.
func (d *Document) Execute(p *Program) (*Result, error) { return d.execute(p, true) }
func (d *Document) execute(p *Program, wantBytes bool) (*Result, error) {
	if p == nil || p.program == nil {
		return nil, errors.New("nil program")
	}
	work := &Document{source: d.source, root: d.root, format: d.format}
	work.runtime = newRuntime(work)
	result := &Result{Values: []any{}}
	if _, err := work.runtime.block(p.program.Statements, false, result); err != nil {
		return nil, err
	}
	if wantBytes {
		result.Bytes = bytes.Clone(work.source)
	}
	result.Changed = !bytes.Equal(d.source, work.source)
	d.source, d.root = work.source, work.root
	return result, nil
}
