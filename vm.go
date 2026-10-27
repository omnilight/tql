package tql

import (
	"fmt"
	"sync"

	"github.com/vasileknik76/tql/internal/bytecode"
	"github.com/vasileknik76/tql/internal/query"
	"github.com/vasileknik76/tql/syntax"
)

type vmOperand struct {
	nodes  []*syntax.Node
	one    [1]*syntax.Node
	value  any
	scalar bool
}

func (o *vmOperand) reset() {
	o.nodes = o.nodes[:0]
	o.value = nil
	o.scalar = false
}
func (o *vmOperand) setNode(node *syntax.Node) {
	o.one[0] = node
	o.nodes = o.one[:]
	o.value = nil
	o.scalar = false
}
func (o *vmOperand) setStream(nodes []*syntax.Node) {
	if len(nodes) == 1 {
		o.setNode(nodes[0])
		return
	}
	o.nodes = nodes
	o.value = nil
	o.scalar = false
}
func (o *vmOperand) setScalar(value any) {
	o.nodes = o.nodes[:0]
	o.value = value
	o.scalar = true
}

type vmFrame struct {
	code *bytecode.Code
	regs []vmOperand
	pc   int
	ctx  *syntax.Node
	main bool
}

type vmScope struct {
	slots []int32
}

type vmLoop struct {
	queue           []*binding
	values          []binding
	refData         []nodeRef
	next            int
	refsStart       int
	entered         bool
	previousCurrent *binding
	scopeDepth      int
	stmtDepth       int
}

type scriptVM struct {
	doc     *Document
	prog    *bytecode.Program
	result  *Result
	slots   []*binding
	scopes  []vmScope
	frames  []*vmFrame
	depth   int
	refs    []*nodeRef
	pathOne [1]*syntax.Node
	current *binding
	steps   int
	stmts   []int32
	loops   []vmLoop
}

var vmPool = sync.Pool{New: func() any { return &scriptVM{} }}

func newVM(doc *Document, inputs map[string]*binding, prog *bytecode.Program) *scriptVM {
	v := vmPool.Get().(*scriptVM)
	v.reset(doc, inputs, prog)
	return v
}

func (v *scriptVM) reset(doc *Document, inputs map[string]*binding, prog *bytecode.Program) {
	v.doc = doc
	v.prog = prog
	v.result = nil
	v.current = nil
	v.steps = 0
	v.depth = 0
	v.pathOne = [1]*syntax.Node{}
	v.refs = v.refs[:0]
	v.stmts = v.stmts[:0]
	if cap(v.slots) < len(prog.Slots) {
		v.slots = make([]*binding, len(prog.Slots))
	} else {
		v.slots = v.slots[:len(prog.Slots)]
		for i := range v.slots {
			v.slots[i] = nil
		}
	}
	if cap(v.loops) < len(prog.Loops) {
		v.loops = make([]vmLoop, len(prog.Loops))
	} else {
		v.loops = v.loops[:len(prog.Loops)]
		for i := range v.loops {
			v.loops[i] = vmLoop{}
		}
	}
	for _, param := range prog.Params {
		slot := indexOfSlot(prog.Slots, param.Name)
		if slot >= 0 {
			v.slots[slot] = inputs[param.Name]
		}
	}
	v.scopes = append(v.scopes[:0], vmScope{slots: prog.Scopes[0].Slots})
	for _, frame := range v.frames {
		for i := range frame.regs {
			frame.regs[i] = vmOperand{}
		}
		frame.code = nil
		frame.ctx = nil
	}
}

func releaseVM(v *scriptVM) {
	v.doc = nil
	v.prog = nil
	v.result = nil
	v.current = nil
	for i := range v.slots {
		v.slots[i] = nil
	}
	for i := range v.loops {
		v.loops[i] = vmLoop{}
	}
	for i := range v.scopes {
		v.scopes[i] = vmScope{}
	}
	v.scopes = v.scopes[:0]
	for _, frame := range v.frames {
		for i := range frame.regs {
			frame.regs[i] = vmOperand{}
		}
		frame.code = nil
		frame.ctx = nil
	}
	v.depth = 0
	if cap(v.refs) > 0 {
		full := v.refs[:cap(v.refs)]
		for i := range full {
			full[i] = nil
		}
	}
	v.refs = v.refs[:0]
	v.stmts = v.stmts[:0]
	v.pathOne = [1]*syntax.Node{}
	vmPool.Put(v)
}

func indexOfSlot(names []string, name string) int32 {
	for i, candidate := range names {
		if candidate == name {
			return int32(i)
		}
	}
	return -1
}

func (v *scriptVM) run(result *Result) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("invalid bytecode: %v", recovered)
		}
	}()
	v.result = result
	if len(v.prog.Codes) == 0 {
		return fmt.Errorf("invalid bytecode: no code")
	}
	f := v.pushFrame(v.prog.Codes[0], nil, true)
	err = v.runFrame(f)
	v.popFrame()
	if err != nil {
		return v.wrap(err)
	}
	return nil
}

func (v *scriptVM) wrap(err error) error {
	if err == nil {
		return nil
	}
	for i := len(v.stmts) - 1; i >= 0; i-- {
		err = fmt.Errorf("statement %d: %w", v.stmts[i], err)
	}
	return err
}

func (v *scriptVM) pushFrame(code *bytecode.Code, ctx *syntax.Node, main bool) *vmFrame {
	if v.depth == len(v.frames) {
		v.frames = append(v.frames, &vmFrame{})
	}
	f := v.frames[v.depth]
	v.depth++
	if cap(f.regs) < code.Registers {
		f.regs = make([]vmOperand, code.Registers)
	} else {
		f.regs = f.regs[:code.Registers]
	}
	for i := range f.regs {
		f.regs[i].reset()
	}
	f.code = code
	f.pc = 0
	f.ctx = ctx
	f.main = main
	return f
}

func (v *scriptVM) popFrame() { v.depth-- }

func (v *scriptVM) evalCode(id int32, ctx *syntax.Node) (vmOperand, error) {
	if id < 0 || int(id) >= len(v.prog.Codes) {
		return vmOperand{}, fmt.Errorf("invalid bytecode: code %d", id)
	}
	f := v.pushFrame(v.prog.Codes[id], ctx, false)
	err := v.runFrame(f)
	var out vmOperand
	if len(f.regs) > 0 {
		out = f.regs[0]
	}
	v.popFrame()
	if err != nil {
		return vmOperand{}, err
	}
	return out, nil
}

func (v *scriptVM) runFrame(f *vmFrame) error {
	for f.pc < len(f.code.Ops) {
		op := f.code.Ops[f.pc]
		f.pc++
		switch op.Code {
		case bytecode.OpConst:
			c := v.prog.Consts[op.B]
			v.reg(f, op.A).setNode(query.Node(c.Value))
		case bytecode.OpConstValue:
			v.reg(f, op.A).setScalar(v.prog.Consts[op.B].Value)
		case bytecode.OpCtx:
			v.reg(f, op.A).setNode(v.currentNode(f))
		case bytecode.OpRoot:
			v.reg(f, op.A).setNode(v.doc.root)
		case bytecode.OpVar:
			b := v.slotBinding(op.B)
			if b == nil {
				return fmt.Errorf("variable $%s has no value", v.slotName(op.B))
			}
			n := v.value(b)
			if n == nil {
				return fmt.Errorf("variable $%s has no value", v.slotName(op.B))
			}
			v.reg(f, op.A).setNode(n)
		case bytecode.OpMissingVar:
			return fmt.Errorf("undefined variable $%s", v.prog.Consts[op.A].Text)
		case bytecode.OpFail:
			return fmt.Errorf("%s", v.prog.Consts[op.A].Text)
		case bytecode.OpOne:
			src := v.stream(f, op.B)
			switch len(src) {
			case 0:
				v.reg(f, op.A).setScalar(nil)
			case 1:
				v.reg(f, op.A).setScalar(src[0].Interface())
			default:
				return fmt.Errorf("expression produced %d values; expected one", len(src))
			}
		case bytecode.OpArg:
			src := v.stream(f, op.B)
			switch len(src) {
			case 0:
				v.reg(f, op.A).setNode(query.Node(nil))
			case 1:
				v.reg(f, op.A).setNode(src[0])
			default:
				return fmt.Errorf("expression produced %d values; expected one", len(src))
			}
		case bytecode.OpNode:
			v.reg(f, op.A).setNode(query.Node(v.scalar(f, op.B)))
		case bytecode.OpTruth:
			v.reg(f, op.A).setScalar(query.Truth(v.scalar(f, op.B)))
		case bytecode.OpAnyTruth:
			truth := false
			for _, node := range v.stream(f, op.B) {
				if query.Truth(node.Interface()) {
					truth = true
					break
				}
			}
			v.reg(f, op.A).setScalar(truth)
		case bytecode.OpSteps:
			plan := v.prog.Paths[op.C]
			nodes, err := v.applyPath(v.stream(f, op.B), &plan)
			if err != nil {
				return err
			}
			v.reg(f, op.A).setStream(nodes)
		case bytecode.OpObject:
			keys := v.prog.Consts[op.B].Texts
			args := v.prog.ArgSets[op.C]
			values := make(map[string]any, len(keys))
			for i, reg := range args {
				values[keys[i]] = v.scalar(f, reg)
			}
			v.reg(f, op.A).setNode(query.Node(values))
		case bytecode.OpArray:
			args := v.prog.ArgSets[op.B]
			values := make([]any, 0)
			for _, reg := range args {
				for _, n := range v.stream(f, reg) {
					values = append(values, n.Interface())
				}
			}
			v.reg(f, op.A).setNode(query.Node(values))
		case bytecode.OpUnary:
			value, err := query.UnaryValue(v.prog.Consts[op.C].Text, v.scalar(f, op.B))
			if err != nil {
				return err
			}
			v.reg(f, op.A).setScalar(value)
		case bytecode.OpBinary:
			value, err := query.Operation(v.prog.Consts[op.D].Text, v.scalar(f, op.B), v.scalar(f, op.C))
			if err != nil {
				return err
			}
			v.reg(f, op.A).setScalar(value)
		case bytecode.OpJump:
			f.pc = int(op.A)
		case bytecode.OpJumpIfFalse:
			if !query.Truth(v.scalar(f, op.A)) {
				f.pc = int(op.B)
			}
		case bytecode.OpJumpIfTrue:
			if query.Truth(v.scalar(f, op.A)) {
				f.pc = int(op.B)
			}
		case bytecode.OpJumpIfNotNull:
			if v.scalar(f, op.A) != nil {
				f.pc = int(op.B)
			}
		case bytecode.OpPipe:
			var out []*syntax.Node
			for _, n := range v.stream(f, op.B) {
				part, err := v.evalCode(op.C, n)
				if err != nil {
					return err
				}
				out = append(out, part.nodes...)
			}
			v.reg(f, op.A).setStream(out)
		case bytecode.OpFilterMatch:
			src := v.stream(f, op.B)
			out := make([]*syntax.Node, 0, len(src))
			plan := v.prog.Matches[op.C]
			for _, n := range src {
				keep, err := v.match(&plan, n)
				if err != nil {
					return err
				}
				if keep {
					out = append(out, n)
				}
			}
			v.reg(f, op.A).setStream(out)
		case bytecode.OpFilterStream:
			out := make([]*syntax.Node, 0, len(v.stream(f, op.B)))
			for _, n := range v.stream(f, op.B) {
				predicate, err := v.evalCode(op.C, n)
				if err != nil {
					return err
				}
				if !predicate.scalar {
					return fmt.Errorf("invalid bytecode: filter predicate is not scalar")
				}
				if query.Truth(predicate.value) {
					out = append(out, n)
				}
			}
			v.reg(f, op.A).setStream(out)
		case bytecode.OpCall:
			args := make([]*syntax.Node, 0, len(v.prog.ArgSets[op.C]))
			for _, reg := range v.prog.ArgSets[op.C] {
				args = append(args, v.stream(f, reg)...)
			}
			nodes, err := query.ApplyBuiltin(v.prog.Consts[op.B].Text, v.currentNode(f), args)
			if err != nil {
				return err
			}
			v.reg(f, op.A).setStream(nodes)
		case bytecode.OpSelect:
			pred, err := v.evalCode(op.B, v.currentNode(f))
			if err != nil {
				return err
			}
			if !pred.scalar {
				return fmt.Errorf("invalid bytecode: select predicate is not scalar")
			}
			if query.Truth(pred.value) {
				v.reg(f, op.A).setNode(v.currentNode(f))
			} else {
				v.reg(f, op.A).setStream(nil)
			}
		case bytecode.OpTree:
			argRegs := v.prog.ArgSets[op.C]
			if len(argRegs) > 1 {
				return fmt.Errorf("%s expects zero or one argument", v.prog.Consts[op.B].Text)
			}
			var arg []*syntax.Node
			hasArg := len(argRegs) == 1
			if hasArg {
				arg = v.stream(f, argRegs[0])
			}
			nodes, err := query.ApplyTreeFunction(v.prog.Consts[op.B].Text, v.currentNode(f), v.doc.root, arg, hasArg)
			if err != nil {
				return err
			}
			v.reg(f, op.A).setStream(nodes)
		case bytecode.OpTick:
			v.steps++
			if v.steps > scriptStepLimit {
				return fmt.Errorf("script execution limit exceeded (%d steps)", scriptStepLimit)
			}
		case bytecode.OpRead:
			for _, n := range v.stream(f, op.A) {
				v.result.Values = append(v.result.Values, n.Interface())
			}
		case bytecode.OpReadValue:
			v.result.Values = append(v.result.Values, v.scalar(f, op.A))
		case bytecode.OpCheckDeclare:
			name := v.prog.Consts[op.A].Text
			if v.scopeHasName(name) {
				return fmt.Errorf("variable $%s is already declared in this scope", name)
			}
		case bytecode.OpDeclare:
			if op.A < 0 || int(op.A) >= len(v.slots) {
				return fmt.Errorf("invalid bytecode: slot %d", op.A)
			}
			v.slots[op.A] = &binding{value: query.Node(v.scalar(f, op.B))}
		case bytecode.OpAssign:
			if err := v.assign(v.prog.Consts[op.A].Text, op.B, op.C, nil, false, f); err != nil {
				return err
			}
		case bytecode.OpAssignConst:
			if err := v.assign(v.prog.Consts[op.A].Text, op.B, -1, v.prog.Consts[op.C].Value, true, f); err != nil {
				return err
			}
		case bytecode.OpDelete:
			if err := v.delete(op.A, f); err != nil {
				return err
			}
		case bytecode.OpForInit:
			if err := v.forInit(op.A, op.B, f); err != nil {
				return err
			}
		case bytecode.OpLoopEnter:
			if op.A < 0 || int(op.A) >= len(v.loops) {
				return fmt.Errorf("invalid bytecode: loop %d", op.A)
			}
			state := &v.loops[op.A]
			state.scopeDepth = len(v.scopes)
			state.stmtDepth = len(v.stmts)
			state.previousCurrent = v.current
		case bytecode.OpForNext:
			done, err := v.forNext(op.A)
			if err != nil {
				return err
			}
			if done {
				f.pc = int(op.B)
				continue
			}
		case bytecode.OpForClose:
			v.forClose(op.A)
		case bytecode.OpLoopBreak, bytecode.OpLoopContinue:
			if op.A < 0 || int(op.A) >= len(v.loops) {
				return fmt.Errorf("invalid bytecode: loop %d", op.A)
			}
			state := &v.loops[op.A]
			v.scopes = v.scopes[:state.scopeDepth]
			v.stmts = v.stmts[:state.stmtDepth]
			state.entered = false
			if op.Code == bytecode.OpLoopBreak {
				f.pc = int(v.prog.Loops[op.A].Break)
			} else {
				f.pc = int(v.prog.Loops[op.A].Continue)
			}
		case bytecode.OpEnterScope:
			if op.A < 0 || int(op.A) >= len(v.prog.Scopes) {
				return fmt.Errorf("invalid bytecode: scope %d", op.A)
			}
			v.enterScope(op.A)
		case bytecode.OpExitScope:
			if len(v.scopes) == 0 {
				return fmt.Errorf("invalid bytecode: scope underflow")
			}
			v.scopes = v.scopes[:len(v.scopes)-1]
		case bytecode.OpStmtEnter:
			v.stmts = append(v.stmts, op.A)
		case bytecode.OpStmtLeave:
			if len(v.stmts) == 0 {
				return fmt.Errorf("invalid bytecode: statement underflow")
			}
			v.stmts = v.stmts[:len(v.stmts)-1]
		case bytecode.OpHalt:
			return nil
		default:
			return fmt.Errorf("invalid bytecode: opcode %d", op.Code)
		}
	}
	return fmt.Errorf("invalid bytecode: code fell through")
}

func (v *scriptVM) reg(f *vmFrame, index int32) *vmOperand {
	if index < 0 || int(index) >= len(f.regs) {
		panic("bytecode: register out of range")
	}
	return &f.regs[index]
}

func (v *scriptVM) stream(f *vmFrame, index int32) []*syntax.Node {
	op := v.reg(f, index)
	if op.scalar {
		panic("bytecode: stream operand is scalar")
	}
	return op.nodes
}

func (v *scriptVM) scalar(f *vmFrame, index int32) any {
	op := v.reg(f, index)
	if !op.scalar {
		panic("bytecode: scalar operand is a stream")
	}
	return op.value
}

func (v *scriptVM) currentNode(f *vmFrame) *syntax.Node {
	if !f.main {
		if f.ctx == nil {
			return query.Node(nil)
		}
		return f.ctx
	}
	if v.current == nil {
		return v.doc.root
	}
	if n := v.value(v.current); n != nil {
		return n
	}
	return query.Node(nil)
}

func (v *scriptVM) slotName(slot int32) string {
	if slot < 0 || int(slot) >= len(v.prog.Slots) {
		return "?"
	}
	return v.prog.Slots[slot]
}

func (v *scriptVM) slotBinding(slot int32) *binding {
	if slot < 0 || int(slot) >= len(v.slots) {
		return nil
	}
	return v.slots[slot]
}

func (v *scriptVM) scopeHasName(name string) bool {
	if len(v.scopes) == 0 {
		return false
	}
	scope := v.scopes[len(v.scopes)-1]
	for _, slot := range scope.slots {
		if v.slots[slot] != nil && v.prog.Slots[slot] == name {
			return true
		}
	}
	return false
}

func (v *scriptVM) enterScope(id int32) {
	scope := v.prog.Scopes[id]
	for _, slot := range scope.Slots {
		v.slots[slot] = nil
	}
	v.scopes = append(v.scopes, vmScope{slots: scope.Slots})
}

func (v *scriptVM) root(owner *binding) *syntax.Node {
	if owner == nil {
		return v.doc.root
	}
	return owner.value
}

func (v *scriptVM) value(b *binding) *syntax.Node {
	if b == nil {
		return nil
	}
	if b.ref == nil {
		return b.value
	}
	if !b.ref.alive {
		return nil
	}
	return locate(v.root(b.ref.owner), b.ref.path)
}

func (v *scriptVM) owner(n *syntax.Node) (*binding, error) {
	top := n
	for top.Parent != nil {
		top = top.Parent
	}
	if top == v.doc.root {
		return nil, nil
	}
	for i := len(v.scopes) - 1; i >= 0; i-- {
		for _, slot := range v.scopes[i].slots {
			b := v.slots[slot]
			if b != nil && b.ref == nil && b.value == top {
				return b, nil
			}
		}
	}
	return nil, fmt.Errorf("target must select a document node or variable")
}

func (v *scriptVM) rebase(owner *binding, op string, a address) {
	if op == "insert" {
		return
	}
	for _, ref := range v.refs {
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

func (v *scriptVM) forInit(loopID, source int32, f *vmFrame) error {
	if loopID < 0 || int(loopID) >= len(v.loops) {
		return fmt.Errorf("invalid bytecode: loop %d", loopID)
	}
	src, err := v.evalCode(source, v.currentNode(f))
	if err != nil {
		return err
	}
	state := &v.loops[loopID]
	state.queue = state.queue[:0]
	state.values = make([]binding, len(src.nodes))
	state.refData = nil
	state.next = 0
	state.entered = false
	state.refsStart = len(v.refs)
	if v.prog.Loops[loopID].ReadOnly {
		for i, n := range src.nodes {
			state.values[i].value = n
			state.queue = append(state.queue, &state.values[i])
		}
		return nil
	}
	state.refData = make([]nodeRef, len(src.nodes))
	for i, n := range src.nodes {
		owner, err := v.owner(n)
		if err != nil {
			state.values[i].value = query.Node(n.Interface())
		} else {
			a, err := addr(v.root(owner), n)
			if err != nil {
				return err
			}
			ref := &state.refData[i]
			ref.owner = owner
			ref.path = a
			ref.alive = true
			state.values[i].ref = ref
			v.refs = append(v.refs, ref)
		}
		state.queue = append(state.queue, &state.values[i])
	}
	return nil
}

func (v *scriptVM) forNext(loopID int32) (bool, error) {
	if loopID < 0 || int(loopID) >= len(v.loops) {
		return false, fmt.Errorf("invalid bytecode: loop %d", loopID)
	}
	state := &v.loops[loopID]
	if state.entered {
		v.scopes = v.scopes[:len(v.scopes)-1]
		state.entered = false
	}
	for state.next < len(state.queue) {
		b := state.queue[state.next]
		state.next++
		if v.value(b) == nil {
			continue
		}
		loop := v.prog.Loops[loopID]
		v.enterScope(loop.Scope)
		v.slots[loop.VarSlot] = b
		v.current = b
		state.entered = true
		return false, nil
	}
	return true, nil
}

func (v *scriptVM) forClose(loopID int32) {
	if loopID < 0 || int(loopID) >= len(v.loops) {
		return
	}
	state := &v.loops[loopID]
	if state.entered {
		v.scopes = v.scopes[:len(v.scopes)-1]
		state.entered = false
	}
	v.current = state.previousCurrent
	if state.refsStart <= len(v.refs) {
		v.refs = v.refs[:state.refsStart]
	}
}
