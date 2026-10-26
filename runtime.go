package tql

import (
	"fmt"

	"github.com/vasileknik76/tql/internal/query"
	"github.com/vasileknik76/tql/syntax"
)

// Script state is private to one execution. Compiled programs contain no values
// or cursors and can therefore be reused concurrently.
type binding struct {
	value *syntax.Node
	ref   *nodeRef
}
type nodeRef struct {
	owner *binding
	path  address
	alive bool
}
type scriptRuntime struct {
	doc     *Document
	scopes  []map[string]*binding
	current *binding
	refs    []*nodeRef
	steps   int
}
type control int

const (
	normal control = iota
	breaking
	continuing
)
const scriptStepLimit = 100000

func newRuntime(d *Document) *scriptRuntime {
	return &scriptRuntime{doc: d, scopes: []map[string]*binding{{}}}
}
func (r *scriptRuntime) tick() error {
	r.steps++
	if r.steps > scriptStepLimit {
		return fmt.Errorf("script execution limit exceeded (%d steps)", scriptStepLimit)
	}
	return nil
}
func (r *scriptRuntime) root(owner *binding) *syntax.Node {
	if owner == nil {
		return r.doc.root
	}
	return owner.value
}
func (r *scriptRuntime) value(b *binding) *syntax.Node {
	if b.ref == nil {
		return b.value
	}
	if !b.ref.alive {
		return nil
	}
	return locate(r.root(b.ref.owner), b.ref.path)
}
func (r *scriptRuntime) context() *syntax.Node {
	if r.current == nil {
		return r.doc.root
	}
	if n := r.value(r.current); n != nil {
		return n
	}
	return query.Node(nil)
}
func (r *scriptRuntime) variables() query.Variables {
	vars := query.Variables{}
	for _, scope := range r.scopes {
		for name, b := range scope {
			vars[name] = r.value(b)
		}
	}
	return vars
}
func (r *scriptRuntime) owner(n *syntax.Node) (*binding, error) {
	top := n
	for top.Parent != nil {
		top = top.Parent
	}
	if top == r.doc.root {
		return nil, nil
	}
	for i := len(r.scopes) - 1; i >= 0; i-- {
		for _, b := range r.scopes[i] {
			if b.ref == nil && b.value == top {
				return b, nil
			}
		}
	}
	return nil, fmt.Errorf("target must select a document node or variable")
}
func (r *scriptRuntime) newRef(owner *binding, a address) *nodeRef {
	ref := &nodeRef{owner: owner, path: append(address{}, a...), alive: true}
	r.refs = append(r.refs, ref)
	return ref
}

// Rebase queued references after structural changes. A deleted node is never
// rebound to the next element that happens to occupy its old array index.
func (r *scriptRuntime) rebase(owner *binding, op string, a address) {
	if op == "insert" {
		return
	}
	for _, ref := range r.refs {
		if !ref.alive || ref.owner != owner {
			continue
		}
		if op == "replace" {
			if len(ref.path) > len(a) && prefix(a, ref.path) {
				ref.alive = false
			}
			continue
		}
		if prefix(a, ref.path) {
			if len(a) == 0 && len(ref.path) == 0 {
				continue
			}
			ref.alive = false
			continue
		}
		if len(a) > 0 {
			last := a[len(a)-1]
			parent := a[:len(a)-1]
			if last.array && len(ref.path) > len(parent) && prefix(parent, ref.path) {
				slot := &ref.path[len(parent)]
				if slot.array && slot.index > last.index {
					slot.index--
				}
			}
		}
	}
}
func (r *scriptRuntime) block(stmts []query.Statement, scoped bool, res *Result) (control, error) {
	if scoped {
		r.scopes = append(r.scopes, map[string]*binding{})
		defer func() { r.scopes = r.scopes[:len(r.scopes)-1] }()
	}
	for i, s := range stmts {
		if err := r.tick(); err != nil {
			return normal, fmt.Errorf("statement %d: %w", i+1, err)
		}
		signal, err := r.execute(s, res)
		if err != nil {
			return normal, fmt.Errorf("statement %d: %w", i+1, err)
		}
		if signal != normal {
			return signal, nil
		}
	}
	return normal, nil
}
func (r *scriptRuntime) execute(s query.Statement, res *Result) (control, error) {
	switch s.Op {
	case "let":
		scope := r.scopes[len(r.scopes)-1]
		if _, exists := scope[s.Name]; exists {
			return normal, fmt.Errorf("variable $%s is already declared in this scope", s.Name)
		}
		v, err := query.One(s.Value, r.context(), r.doc.root, r.variables())
		if err != nil {
			return normal, err
		}
		scope[s.Name] = &binding{value: query.Node(v)}
		return normal, nil
	case "if":
		v, err := query.One(s.Value, r.context(), r.doc.root, r.variables())
		if err != nil {
			return normal, err
		}
		body := s.Else
		if query.Truth(v) {
			body = s.Body
		}
		return r.block(body, true, res)
	case "while":
		for {
			if err := r.tick(); err != nil {
				return normal, err
			}
			v, err := query.One(s.Value, r.context(), r.doc.root, r.variables())
			if err != nil {
				return normal, err
			}
			if !query.Truth(v) {
				return normal, nil
			}
			signal, err := r.block(s.Body, true, res)
			if err != nil {
				return normal, err
			}
			if signal == breaking {
				return normal, nil
			}
		}
	case "for":
		return r.iterate(s, res)
	case "break":
		return breaking, nil
	case "continue":
		return continuing, nil
	default:
		return normal, r.doc.statement(s, res)
	}
}
func (r *scriptRuntime) iterate(s query.Statement, res *Result) (control, error) {
	ns, err := s.Value.Eval(r.context(), r.doc.root, r.variables())
	if err != nil {
		return normal, err
	}
	start := len(r.refs)
	defer func() { r.refs = r.refs[:start] }()
	queue := make([]*binding, 0, len(ns))
	for _, n := range ns {
		owner, err := r.owner(n)
		if err != nil {
			queue = append(queue, &binding{value: query.Node(n.Interface())})
			continue
		}
		a, err := addr(r.root(owner), n)
		if err != nil {
			return normal, err
		}
		queue = append(queue, &binding{ref: r.newRef(owner, a)})
	}
	previous := r.current
	defer func() { r.current = previous }()
	for _, b := range queue {
		if err := r.tick(); err != nil {
			return normal, err
		}
		if r.value(b) == nil {
			continue
		}
		r.current = b
		r.scopes = append(r.scopes, map[string]*binding{s.Name: b})
		signal, err := r.block(s.Body, false, res)
		r.scopes = r.scopes[:len(r.scopes)-1]
		if err != nil {
			return normal, err
		}
		if signal == breaking {
			break
		}
	}
	return normal, nil
}
