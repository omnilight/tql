package tql

import (
	"bytes"
	"fmt"

	"github.com/vasileknik76/tql/internal/editmodel"
	"github.com/vasileknik76/tql/internal/query"
	"github.com/vasileknik76/tql/syntax"
)

// Apply executes structured commands in order, committing only if every command
// succeeds. Commands are constructed with package edit (or syntax). Paths resolve
// against the result of the previous command. An empty list changes nothing.
func (d *Document) Apply(operations ...syntax.Operation) (*Result, error) {
	if d == nil || d.root == nil || d.format == nil {
		return nil, fmt.Errorf("uninitialized document")
	}
	for i, op := range operations {
		if err := editmodel.Validate(op); err != nil {
			return nil, operationError(i, op, err)
		}
	}
	work := &Document{source: d.source, root: d.root, format: d.format}
	for i, op := range operations {
		if err := work.applyOperation(op); err != nil {
			return nil, operationError(i, op, err)
		}
	}
	result := &Result{Values: []any{}, Bytes: bytes.Clone(work.source), Changed: !bytes.Equal(d.source, work.source)}
	if len(operations) > 0 {
		d.source, d.root = work.source, work.root
	}
	return result, nil
}

func operationError(index int, op syntax.Operation, err error) error {
	return fmt.Errorf("operation %d (%s %s): %w", index, op.Kind(), op.Path(), err)
}

func (d *Document) applyOperation(op syntax.Operation) error {
	steps := editmodel.Steps(op.Path())
	n, err := typedTarget(d.root, steps)
	if err != nil {
		return err
	}
	if op.Kind() == syntax.DelOperation {
		if n == nil {
			return nil
		}
		return d.mutate("delete", n, "", nil)
	}
	v := op.Value()
	if n != nil {
		if query.Equal(n.Interface(), v) {
			return nil
		}
		return d.mutate("replace", n, "", v)
	}
	path := make([]query.Step, len(steps))
	for i, step := range steps {
		if step.Array {
			path[i] = query.Step{Kind: "index", Index: step.Index}
		} else {
			path[i] = query.Step{Kind: "key", Key: step.Key}
		}
	}
	return d.createFrom(nil, d.root, path, v)
}

func typedTarget(n *syntax.Node, steps []editmodel.Step) (*syntax.Node, error) {
	for _, step := range steps {
		if n == nil || n.Kind == syntax.Null {
			return nil, nil
		}
		if step.Array {
			if n.Kind != syntax.Array {
				return nil, fmt.Errorf("cannot index %s", n.Kind)
			}
			if step.Index >= len(n.Items) {
				return nil, nil
			}
			n = n.Items[step.Index]
		} else {
			if n.Kind != syntax.Object {
				return nil, fmt.Errorf("cannot access field %q on %s", step.Key, n.Kind)
			}
			n = n.Get(step.Key)
		}
	}
	return n, nil
}
