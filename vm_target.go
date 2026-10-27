package tql

import (
	"fmt"
	"strings"

	"github.com/vasileknik76/tql/internal/bytecode"
	"github.com/vasileknik76/tql/internal/query"
	"github.com/vasileknik76/tql/syntax"
)

func (v *scriptVM) applyPath(nodes []*syntax.Node, plan *bytecode.PathPlan) ([]*syntax.Node, error) {
	current := nodes
	for _, step := range plan.Steps {
		if len(current) == 1 && (step.Kind == bytecode.StepKey || step.Kind == bytecode.StepIndex) {
			out := v.pathOne[:0]
			var err error
			out, err = v.appendStep(out, step, current[0])
			if err != nil {
				return nil, err
			}
			current = out
			continue
		}
		next := make([]*syntax.Node, 0, pathCapacity(current, step))
		for _, n := range current {
			var err error
			next, err = v.appendStep(next, step, n)
			if err != nil {
				return nil, err
			}
		}
		current = next
	}
	return current, nil
}

func (v *scriptVM) appendStep(out []*syntax.Node, step bytecode.Step, n *syntax.Node) ([]*syntax.Node, error) {
	switch step.Kind {
	case bytecode.StepKey:
		if n.Kind == syntax.Null {
			return out, nil
		}
		if n.Kind != syntax.Object {
			return nil, fmt.Errorf("cannot access field %q on %s", step.Key, n.Kind)
		}
		if value := n.Get(step.Key); value != nil {
			out = append(out, value)
		}
		return out, nil
	case bytecode.StepIndex:
		if n.Kind == syntax.Null {
			return out, nil
		}
		if n.Kind != syntax.Array {
			return nil, fmt.Errorf("cannot index %s", n.Kind)
		}
		index := step.Index
		if index < 0 {
			index += len(n.Items)
		}
		if index >= 0 && index < len(n.Items) {
			out = append(out, n.Items[index])
		}
		return out, nil
	case bytecode.StepWildcard:
		switch n.Kind {
		case syntax.Array:
			out = append(out, n.Items...)
		case syntax.Object:
			for _, entry := range n.Entries {
				out = append(out, entry.Value)
			}
		default:
			return nil, fmt.Errorf("cannot iterate %s", n.Kind)
		}
		return out, nil
	case bytecode.StepSlice:
		if n.Kind != syntax.Array {
			return nil, fmt.Errorf("cannot slice %s", n.Kind)
		}
		lo, hi := 0, len(n.Items)
		if step.HasFrom {
			lo = pathBound(step.From, hi)
		}
		if step.HasTo {
			hi = pathBound(step.To, len(n.Items))
		}
		if hi < lo {
			hi = lo
		}
		out = append(out, n.Items[lo:hi]...)
		return out, nil
	case bytecode.StepDynamic:
		selector, err := v.evalCode(step.Code, n)
		if err != nil {
			return nil, err
		}
		if len(selector.nodes) != 1 {
			return nil, fmt.Errorf("dynamic selector requires exactly one string or integer")
		}
		q, err := query.SelectorStep(selector.nodes[0].Interface())
		if err != nil {
			return nil, err
		}
		resolved := bytecode.Step{Kind: bytecode.StepKey, Key: q.Key}
		if q.Kind == "index" {
			resolved = bytecode.Step{Kind: bytecode.StepIndex, Index: q.Index}
		}
		return v.appendStep(out, resolved, n)
	case bytecode.StepFilter:
		switch n.Kind {
		case syntax.Array:
			for _, item := range n.Items {
				keep, err := v.filterValue(step.Code, item)
				if err != nil {
					return nil, err
				}
				if keep {
					out = append(out, item)
				}
			}
		case syntax.Object:
			for _, entry := range n.Entries {
				keep, err := v.filterValue(step.Code, entry.Value)
				if err != nil {
					return nil, err
				}
				if keep {
					out = append(out, entry.Value)
				}
			}
		default:
			return nil, fmt.Errorf("cannot iterate %s", n.Kind)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("invalid bytecode: path step %d", step.Kind)
	}
}

func pathCapacity(nodes []*syntax.Node, step bytecode.Step) int {
	// Key, index and dynamic steps usually emit at most one value per input.
	// Starting small avoids allocating a full-size slice for the common
	// "filter many, select one" path ending in a key.
	if step.Kind == bytecode.StepKey || step.Kind == bytecode.StepIndex || step.Kind == bytecode.StepDynamic {
		return len(nodes)
	}
	capacity := len(nodes)
	estimated := 0
	for _, n := range nodes {
		count := 0
		switch step.Kind {
		case bytecode.StepWildcard:
			switch n.Kind {
			case syntax.Array:
				count = len(n.Items)
			case syntax.Object:
				count = len(n.Entries)
			}
		case bytecode.StepFilter:
			switch n.Kind {
			case syntax.Array:
				count = len(n.Items)
			case syntax.Object:
				count = len(n.Entries)
			}
			// Filters often select a small subset; do not reserve the whole
			// container. A larger result grows normally.
			if count > 32 {
				count = 32
			}
		case bytecode.StepSlice:
			if n.Kind == syntax.Array {
				hi := len(n.Items)
				if step.HasTo {
					hi = pathBound(step.To, len(n.Items))
				}
				lo := 0
				if step.HasFrom {
					lo = pathBound(step.From, len(n.Items))
				}
				if hi > lo {
					count = hi - lo
				}
			}
		}
		if count > estimated {
			estimated = count
		}
	}
	if estimated > capacity {
		capacity = estimated
	}
	return capacity
}

func (v *scriptVM) filterValue(code int32, item *syntax.Node) (bool, error) {
	result, err := v.evalCode(code, item)
	if err != nil {
		return false, err
	}
	if result.scalar {
		return query.Truth(result.value), nil
	}
	for _, value := range result.nodes {
		if query.Truth(value.Interface()) {
			return true, nil
		}
	}
	return false, nil
}

func pathBound(i, n int) int {
	if i < 0 {
		i += n
	}
	if i < 0 {
		return 0
	}
	if i > n {
		return n
	}
	return i
}

func (v *scriptVM) applyStep(step bytecode.Step, n *syntax.Node) ([]*syntax.Node, error) {
	return v.appendStep(nil, step, n)
}

func (v *scriptVM) resolveStep(step bytecode.Step, n *syntax.Node) (query.Step, error) {
	switch step.Kind {
	case bytecode.StepKey:
		return query.Step{Kind: "key", Key: step.Key}, nil
	case bytecode.StepIndex:
		return query.Step{Kind: "index", Index: step.Index}, nil
	case bytecode.StepWildcard:
		return query.Step{Kind: "wildcard"}, nil
	case bytecode.StepSlice:
		q := query.Step{Kind: "slice"}
		if step.HasFrom {
			from := step.From
			q.From = &from
		}
		if step.HasTo {
			to := step.To
			q.To = &to
		}
		return q, nil
	case bytecode.StepDynamic:
		selector, err := v.evalCode(step.Code, n)
		if err != nil {
			return query.Step{}, err
		}
		if len(selector.nodes) != 1 {
			return query.Step{}, fmt.Errorf("dynamic selector requires exactly one string or integer")
		}
		return query.SelectorStep(selector.nodes[0].Interface())
	case bytecode.StepFilter:
		return query.Step{Kind: "filter"}, nil
	default:
		return query.Step{}, fmt.Errorf("invalid bytecode: path step %d", step.Kind)
	}
}

func (v *scriptVM) assignmentPath(base *syntax.Node, plan *bytecode.PathPlan, creating bool) ([]*syntax.Node, []creation, error) {
	var targets []*syntax.Node
	var pending []creation
	var walk func(*syntax.Node, []bytecode.Step) error
	walk = func(n *syntax.Node, rest []bytecode.Step) error {
		if len(rest) == 0 {
			targets = append(targets, n)
			return nil
		}
		step, err := v.resolveStep(rest[0], n)
		if err != nil {
			return err
		}
		missing := false
		if step.Kind == "key" {
			if n.Kind == syntax.Null {
				missing = true
			} else if n.Kind != syntax.Object {
				return fmt.Errorf("cannot create field %q on %s", step.Key, n.Kind)
			} else {
				missing = n.Get(step.Key) == nil
			}
		}
		if step.Kind == "index" {
			if n.Kind == syntax.Null {
				missing = true
			} else if n.Kind != syntax.Array {
				return fmt.Errorf("cannot index %s", n.Kind)
			} else {
				index := step.Index
				if index < 0 {
					index += len(n.Items)
				}
				if index < 0 || index > len(n.Items) {
					return fmt.Errorf("array index %d out of bounds", step.Index)
				}
				missing = index == len(n.Items)
			}
		}
		if missing && creating {
			concrete := make([]query.Step, len(rest))
			for i, item := range rest {
				resolved, err := v.resolveStep(item, n)
				if err != nil {
					return err
				}
				if resolved.Kind != "key" && resolved.Kind != "index" {
					return nil
				}
				concrete[i] = resolved
			}
			owner, err := v.owner(n)
			if err != nil {
				return err
			}
			if err := writable(owner); err != nil {
				return err
			}
			a, err := addr(v.root(owner), n)
			if err != nil {
				return err
			}
			pending = append(pending, creation{owner: owner, parent: a, steps: concrete})
			return nil
		}
		children, err := v.applyStep(rest[0], n)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := walk(child, rest[1:]); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(base, plan.Steps); err != nil {
		return nil, nil, err
	}
	return targets, pending, nil
}

func (v *scriptVM) assign(op string, targetID, rhsID int32, rhsValue any, hasValue bool, f *vmFrame) error {
	if targetID < 0 || int(targetID) >= len(v.prog.Targets) {
		return fmt.Errorf("invalid bytecode: target %d", targetID)
	}
	target := v.prog.Targets[targetID]
	var nodes []*syntax.Node
	var pending []creation
	creating := op == "=" || op == "??="
	if target.Kind == bytecode.TargetPath {
		var bases []*syntax.Node
		switch {
		case target.Root:
			bases = []*syntax.Node{v.doc.root}
		case target.Base >= 0:
			base, err := v.evalCode(target.Base, v.currentNode(f))
			if err != nil {
				return err
			}
			bases = append([]*syntax.Node(nil), base.nodes...)
		default:
			bases = []*syntax.Node{v.currentNode(f)}
		}
		plan := v.prog.Paths[target.Path]
		for _, base := range bases {
			if creating {
				targets, creates, err := v.assignmentPath(base, &plan, true)
				if err != nil {
					return err
				}
				nodes = append(nodes, targets...)
				pending = append(pending, creates...)
			} else {
				targets, err := v.applyPath([]*syntax.Node{base}, &plan)
				if err != nil {
					return err
				}
				nodes = append(nodes, targets...)
			}
		}
	} else {
		targets, err := v.evalCode(target.Base, v.currentNode(f))
		if err != nil {
			return err
		}
		nodes = targets.nodes
	}

	groups, err := v.doc.groups(nodes, false)
	if err != nil {
		return err
	}
	var updates []assignment
	for _, group := range groups {
		for _, path := range group.paths {
			n := locate(v.root(group.owner), path)
			if n == nil {
				return fmt.Errorf("assignment target disappeared")
			}
			if op == "??=" && n.Kind != syntax.Null {
				continue
			}
			rhsCtx := v.currentNode(f)
			if op == "|=" {
				rhsCtx = n
			}
			value := rhsValue
			var err error
			if !hasValue {
				value, err = v.evalScalar(rhsID, rhsCtx)
				if err != nil {
					return err
				}
			}
			if op != "=" && op != "|=" && op != "??=" {
				value, err = query.Operation(strings.TrimSuffix(op, "="), n.Interface(), value)
				if err != nil {
					return err
				}
			}
			if !query.Equal(n.Interface(), value) {
				updates = append(updates, assignment{owner: group.owner, path: path, value: value})
			}
		}
	}
	var pendingValue any
	if len(pending) > 0 {
		pendingValue = rhsValue
		if !hasValue {
			pendingValue, err = v.evalScalar(rhsID, v.currentNode(f))
			if err != nil {
				return err
			}
		}
	}
	if err := v.doc.replaceMany(updates); err != nil {
		return err
	}
	for _, item := range pending {
		parent := locate(v.root(item.owner), item.parent)
		if parent == nil {
			return fmt.Errorf("insertion parent disappeared")
		}
		if err := v.doc.createFrom(item.owner, parent, item.steps, pendingValue); err != nil {
			return err
		}
	}
	return nil
}

func (v *scriptVM) evalScalar(code int32, ctx *syntax.Node) (any, error) {
	value, err := v.evalCode(code, ctx)
	if err != nil {
		return nil, err
	}
	if !value.scalar {
		return nil, fmt.Errorf("invalid bytecode: expression is not scalar")
	}
	return value.value, nil
}

func (v *scriptVM) delete(setID int32, f *vmFrame) error {
	if setID < 0 || int(setID) >= len(v.prog.TargetSets) {
		return fmt.Errorf("invalid bytecode: delete set %d", setID)
	}
	var nodes []*syntax.Node
	for _, code := range v.prog.TargetSets[setID] {
		value, err := v.evalCode(code, v.currentNode(f))
		if err != nil {
			return err
		}
		nodes = append(nodes, value.nodes...)
	}
	groups, err := v.doc.groups(nodes, true)
	if err != nil {
		return err
	}
	for _, group := range groups {
		for _, path := range group.paths {
			n := locate(v.root(group.owner), path)
			if n == nil {
				return fmt.Errorf("deletion target disappeared")
			}
			if err := v.doc.mutateOwned(group.owner, "delete", n, "", nil); err != nil {
				return err
			}
		}
	}
	return nil
}
