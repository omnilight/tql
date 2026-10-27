package bytecode

import (
	"fmt"
	"slices"

	"github.com/vasileknik76/tql/internal/query"
)

type scopeInfo struct {
	id    int32
	names map[string]int32
}

type compiler struct {
	prog   *Program
	code   *Code
	scopes []*scopeInfo
	loops  []int32
	used   []bool
	stmt   int32
	err    error
}

// Compile lowers a parsed TQL program to bytecode.
func Compile(p *query.Program) (*Program, error) {
	if p == nil {
		return nil, fmt.Errorf("bytecode: nil program")
	}
	prog := &Program{
		Codes:   []*Code{{Name: "main"}},
		Scopes:  []Scope{{}},
		Mutates: p.Mutates(),
	}
	c := &compiler{prog: prog, code: prog.Codes[0], scopes: []*scopeInfo{{id: 0, names: map[string]int32{}}}}
	for _, param := range p.Parameters {
		value := any(nil)
		if param.HasDefault {
			var err error
			value, err = NormalizeParamDefault(param.Default)
			if err != nil {
				return nil, fmt.Errorf("parameter $%s: %w", param.Name, err)
			}
		}
		slot := c.addSlot(param.Name)
		c.scopes[0].names[param.Name] = slot
		prog.Params = append(prog.Params, Param{Name: param.Name, Default: value, HasDefault: param.HasDefault})
	}
	if err := c.compileStatements(p.Statements); err != nil {
		return nil, err
	}
	c.emit(Op{Code: OpHalt})
	if c.err != nil {
		return nil, c.err
	}
	return prog, nil
}

func (c *compiler) emit(op Op) int32 {
	op.Stmt = c.stmt
	c.code.Ops = append(c.code.Ops, op)
	return int32(len(c.code.Ops) - 1)
}

func (c *compiler) patchA(at int32, target int32) { c.code.Ops[at].A = target }
func (c *compiler) patchB(at int32, target int32) { c.code.Ops[at].B = target }

func (c *compiler) jump(op OpCode, a int32) int32 { return c.emit(Op{Code: op, A: a}) }
func (c *compiler) jumpIf(op OpCode, src int32) int32 {
	return c.emit(Op{Code: op, A: src})
}

func (c *compiler) scope() *scopeInfo { return c.scopes[len(c.scopes)-1] }

func (c *compiler) newScope() int32 {
	id := int32(len(c.prog.Scopes))
	c.prog.Scopes = append(c.prog.Scopes, Scope{})
	c.scopes = append(c.scopes, &scopeInfo{id: id, names: map[string]int32{}})
	return id
}

func (c *compiler) enterScope(id int32) { c.emit(Op{Code: OpEnterScope, A: id}) }
func (c *compiler) exitScope()          { c.emit(Op{Code: OpExitScope}) }

func (c *compiler) addSlot(name string) int32 {
	slot := int32(len(c.prog.Slots))
	c.prog.Slots = append(c.prog.Slots, name)
	scope := c.scope()
	c.prog.Scopes[scope.id].Slots = append(c.prog.Scopes[scope.id].Slots, slot)
	return slot
}

func (c *compiler) lookup(name string) (int32, bool) {
	for _, v := range slices.Backward(c.scopes) {
		if slot, ok := v.names[name]; ok {
			return slot, true
		}
	}
	return 0, false
}

func (c *compiler) alloc() int32 {
	for i, used := range c.used {
		if !used {
			c.used[i] = true
			return int32(i)
		}
	}
	i := int32(len(c.used))
	c.used = append(c.used, true)
	if int(i)+1 > c.code.Registers {
		c.code.Registers = int(i) + 1
	}
	return i
}

func (c *compiler) release(reg int32) {
	if reg >= 0 && int(reg) < len(c.used) {
		c.used[reg] = false
	}
}

func (c *compiler) reserve(reg int32) {
	for int(reg) >= len(c.used) {
		c.used = append(c.used, false)
	}
	c.used[reg] = true
	if int(reg)+1 > c.code.Registers {
		c.code.Registers = int(reg) + 1
	}
}

func (c *compiler) newLoop(kind LoopKind, scope int32, varSlot int32) int32 {
	id := int32(len(c.prog.Loops))
	c.prog.Loops = append(c.prog.Loops, Loop{Kind: kind, Scope: scope, VarSlot: varSlot, Continue: -1, Break: -1})
	return id
}

func (c *compiler) compileStatements(stmts []query.Statement) error {
	for i, stmt := range stmts {
		if err := c.compileStatement(stmt, int32(i+1)); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileStatement(s query.Statement, index int32) error {
	c.stmt = index
	c.emit(Op{Code: OpStmtEnter, A: index})
	c.emit(Op{Code: OpTick})
	switch s.Op {
	case "let":
		if err := c.compileLet(s); err != nil {
			return err
		}
	case "if":
		if err := c.compileIf(s); err != nil {
			return err
		}
	case "while":
		if err := c.compileWhile(s); err != nil {
			return err
		}
	case "for":
		if err := c.compileFor(s); err != nil {
			return err
		}
	case "break", "continue":
		if len(c.loops) == 0 {
			return fmt.Errorf("bytecode: %s outside a loop", s.Op)
		}
		op := OpLoopBreak
		if s.Op == "continue" {
			op = OpLoopContinue
		}
		c.emit(Op{Code: op, A: c.loops[len(c.loops)-1]})
	case "read":
		if err := c.compileRead(s); err != nil {
			return err
		}
	case "del":
		if err := c.compileDelete(s); err != nil {
			return err
		}
	default:
		if err := c.compileAssign(s); err != nil {
			return err
		}
	}
	c.emit(Op{Code: OpStmtLeave})
	return nil
}

func (c *compiler) compileLet(s query.Statement) error {
	name := c.prog.AddText(s.Name)
	c.emit(Op{Code: OpCheckDeclare, A: name})
	slot := c.addSlot(s.Name)
	rhs := c.alloc()
	if err := c.compileScalar(s.Value, rhs); err != nil {
		return err
	}
	c.emit(Op{Code: OpDeclare, A: slot, B: rhs})
	c.release(rhs)
	c.scope().names[s.Name] = slot
	return nil
}

func (c *compiler) compileIf(s query.Statement) error {
	cond := c.alloc()
	if err := c.compileScalar(s.Value, cond); err != nil {
		return err
	}
	toElse := c.jumpIf(OpJumpIfFalse, cond)
	c.release(cond)

	bodyScope := c.newScope()
	c.enterScope(bodyScope)
	if err := c.compileStatements(s.Body); err != nil {
		return err
	}
	c.exitScope()
	c.scopes = c.scopes[:len(c.scopes)-1]
	toEnd := c.jump(OpJump, 0)

	c.patchB(toElse, int32(len(c.code.Ops)))
	if len(s.Else) > 0 {
		elseScope := c.newScope()
		c.enterScope(elseScope)
		if err := c.compileStatements(s.Else); err != nil {
			return err
		}
		c.exitScope()
		c.scopes = c.scopes[:len(c.scopes)-1]
	}
	c.patchA(toEnd, int32(len(c.code.Ops)))
	return nil
}

func (c *compiler) compileWhile(s query.Statement) error {
	loop := c.newLoop(LoopWhile, -1, -1)
	c.emit(Op{Code: OpLoopEnter, A: loop})
	start := int32(len(c.code.Ops))
	c.emit(Op{Code: OpTick})
	cond := c.alloc()
	if err := c.compileScalar(s.Value, cond); err != nil {
		return err
	}
	toEnd := c.jumpIf(OpJumpIfFalse, cond)
	c.release(cond)

	bodyScope := c.newScope()
	c.enterScope(bodyScope)
	c.loops = append(c.loops, loop)
	if err := c.compileStatements(s.Body); err != nil {
		return err
	}
	c.loops = c.loops[:len(c.loops)-1]
	c.exitScope()
	c.emit(Op{Code: OpJump, A: start})
	end := int32(len(c.code.Ops))
	c.patchB(toEnd, end)
	c.prog.Loops[loop].Continue = start
	c.prog.Loops[loop].Break = end
	c.scopes = c.scopes[:len(c.scopes)-1]
	return nil
}

func (c *compiler) compileFor(s query.Statement) error {
	source := c.subStream(s.Value, "for-source")
	loopScope := c.newScope()
	c.scope().names[s.Name] = c.addSlot(s.Name)
	loop := c.newLoop(LoopFor, loopScope, c.scope().names[s.Name])
	c.prog.Loops[loop].ReadOnly = !statementsMutate(s.Body)
	c.emit(Op{Code: OpForInit, A: loop, B: source})
	c.emit(Op{Code: OpLoopEnter, A: loop})
	next := int32(len(c.code.Ops))
	c.emit(Op{Code: OpTick})
	toClose := c.emit(Op{Code: OpForNext, A: loop})
	c.loops = append(c.loops, loop)
	if err := c.compileStatements(s.Body); err != nil {
		return err
	}
	c.loops = c.loops[:len(c.loops)-1]
	c.emit(Op{Code: OpJump, A: next})
	closeAt := int32(len(c.code.Ops))
	c.patchB(toClose, closeAt)
	c.emit(Op{Code: OpForClose, A: loop})
	c.prog.Loops[loop].Continue = next
	c.prog.Loops[loop].Break = closeAt
	c.scopes = c.scopes[:len(c.scopes)-1]
	return nil
}

func (c *compiler) compileRead(s query.Statement) error {
	src := c.alloc()
	if scalarExpression(s.Target) {
		if err := c.compileScalar(s.Target, src); err != nil {
			return err
		}
		c.emit(Op{Code: OpReadValue, A: src})
	} else {
		if err := c.compileStream(s.Target, src); err != nil {
			return err
		}
		c.emit(Op{Code: OpRead, A: src})
	}
	c.release(src)
	return nil
}

func scalarExpression(e query.Expr) bool {
	switch x := e.(type) {
	case query.Literal, query.Unary:
		return true
	case query.Binary:
		return x.Op != "|"
	default:
		return false
	}
}

func (c *compiler) compileDelete(s query.Statement) error {
	codes := make([]int32, 0, len(s.Targets))
	for _, target := range s.Targets {
		codes = append(codes, c.subStream(target, "delete-target"))
	}
	set := c.prog.AddTargetSet(codes)
	c.emit(Op{Code: OpDelete, A: set})
	return nil
}

func (c *compiler) compileAssign(s query.Statement) error {
	target := c.compileTarget(s.Target, s.Op)
	op := c.prog.AddText(s.Op)
	if literal, ok := s.Value.(query.Literal); ok {
		value := c.prog.AddValue(literal.Value)
		c.emit(Op{Code: OpAssignConst, A: op, B: target, C: value})
		return nil
	}
	rhs := c.subScalar(s.Value, "assign-rhs")
	c.emit(Op{Code: OpAssign, A: op, B: target, C: rhs})
	return nil
}

func (c *compiler) compileTarget(e query.Expr, op string) int32 {
	if path, ok := e.(query.Path); ok && (op == "=" || op == "??=") {
		target := TargetPlan{Kind: TargetPath, Base: -1, Path: -1, Root: path.Root}
		if !path.Root && path.Base != nil {
			target.Base = c.subStream(path.Base, "target-base")
		}
		if len(path.Steps) > 0 {
			target.Path = c.path(path.Steps)
		} else {
			target.Path = c.prog.AddPath(PathPlan{})
		}
		return c.prog.AddTarget(target)
	}
	return c.prog.AddTarget(TargetPlan{Kind: TargetExpr, Base: c.subStream(e, "target"), Path: -1})
}

func (c *compiler) path(steps []query.Step) int32 {
	plan := PathPlan{}
	if len(steps) > 0 {
		plan.Steps = make([]Step, 0, len(steps))
	}
	for _, step := range steps {
		item := Step{Kind: StepKey}
		switch step.Kind {
		case "key":
			item.Kind = StepKey
			item.Key = step.Key
		case "index":
			item.Kind = StepIndex
			item.Index = step.Index
		case "wildcard":
			item.Kind = StepWildcard
		case "slice":
			item.Kind = StepSlice
			if step.From != nil {
				item.HasFrom = true
				item.From = *step.From
			}
			if step.To != nil {
				item.HasTo = true
				item.To = *step.To
			}
		case "filter":
			if match, ok := c.matchPlan(step.Predicate); ok {
				item.Kind = StepMatch
				item.Code = c.prog.AddMatch(match)
			} else {
				item.Kind = StepFilter
				item.Code = c.subTruth(step.Predicate, "filter")
			}
		case "dynamic":
			item.Kind = StepDynamic
			item.Code = c.subStream(step.Predicate, "selector")
		default:
			panic("bytecode: unknown path step " + step.Kind)
		}
		plan.Steps = append(plan.Steps, item)
	}
	return c.prog.AddPath(plan)
}

func (c *compiler) subStream(e query.Expr, name string) int32 {
	return c.sub(e, false, name)
}

func (c *compiler) subScalar(e query.Expr, name string) int32 {
	return c.sub(e, true, name)
}

func (c *compiler) subTruth(e query.Expr, name string) int32 {
	code := &Code{Name: name}
	child := &compiler{prog: c.prog, code: code, scopes: c.scopes, stmt: c.stmt}
	child.reserve(0)
	if err := child.compileTruth(e, 0); err != nil {
		c.err = err
		return -1
	}
	child.emit(Op{Code: OpHalt})
	code.Registers = child.code.Registers
	c.prog.Codes = append(c.prog.Codes, code)
	return int32(len(c.prog.Codes) - 1)
}

func (c *compiler) compileTruth(e query.Expr, dst int32) error {
	switch e.(type) {
	case query.Unary, query.Binary:
		if binary, ok := e.(query.Binary); ok && binary.Op == "|" {
			break
		}
		tmp := c.alloc()
		if err := c.compileScalar(e, tmp); err != nil {
			return err
		}
		c.emit(Op{Code: OpTruth, A: dst, B: tmp})
		c.release(tmp)
		return nil
	}
	tmp := c.alloc()
	if err := c.compileStream(e, tmp); err != nil {
		return err
	}
	c.emit(Op{Code: OpAnyTruth, A: dst, B: tmp})
	c.release(tmp)
	return nil
}

func (c *compiler) sub(e query.Expr, scalar bool, name string) int32 {
	code := &Code{Name: name}
	child := &compiler{prog: c.prog, code: code, scopes: c.scopes, stmt: c.stmt}
	child.reserve(0)
	if scalar {
		if err := child.compileScalar(e, 0); err != nil {
			c.err = err
			return -1
		}
	} else {
		if err := child.compileStream(e, 0); err != nil {
			c.err = err
			return -1
		}
	}
	child.emit(Op{Code: OpHalt})
	code.Registers = child.code.Registers
	c.prog.Codes = append(c.prog.Codes, code)
	return int32(len(c.prog.Codes) - 1)
}

func (c *compiler) compileStream(e query.Expr, dst int32) error {
	switch x := e.(type) {
	case query.Literal:
		c.emit(Op{Code: OpConst, A: dst, B: c.prog.AddValue(x.Value)})
		return nil
	case query.Variable:
		if slot, ok := c.lookup(x.Name); ok {
			c.emit(Op{Code: OpVar, A: dst, B: slot})
		} else {
			c.emit(Op{Code: OpMissingVar, A: c.prog.AddText(x.Name)})
		}
		return nil
	case query.Path:
		base := dst
		switch {
		case x.Root:
			c.emit(Op{Code: OpRoot, A: base})
		case x.Base != nil:
			if err := c.compileStream(x.Base, base); err != nil {
				return err
			}
		default:
			c.emit(Op{Code: OpCtx, A: base})
		}
		if len(x.Steps) > 0 {
			src := base
			if x.Base != nil || x.Root {
				// Keep the base in dst and map steps in place; the VM reads the
				// source slice before writing the destination.
				src = base
			}
			c.emit(Op{Code: OpSteps, A: dst, B: src, C: c.path(x.Steps)})
		}
		return nil
	case query.Object:
		keys := make([]string, len(x.Keys))
		copy(keys, x.Keys)
		keySet := c.prog.AddConst(Const{Texts: keys})
		args := make([]int32, 0, len(x.Values))
		for _, value := range x.Values {
			reg := c.alloc()
			if err := c.compileScalar(value, reg); err != nil {
				return err
			}
			args = append(args, reg)
		}
		c.emit(Op{Code: OpObject, A: dst, B: keySet, C: c.prog.AddArgSet(args)})
		for _, reg := range args {
			c.release(reg)
		}
		return nil
	case query.Array:
		args := make([]int32, 0, len(x.Values))
		for _, value := range x.Values {
			reg := c.alloc()
			if err := c.compileStream(value, reg); err != nil {
				return err
			}
			args = append(args, reg)
		}
		c.emit(Op{Code: OpArray, A: dst, B: c.prog.AddArgSet(args)})
		for _, reg := range args {
			c.release(reg)
		}
		return nil
	case query.Call:
		return c.compileCall(x, dst)
	case query.Binary:
		if x.Op == "|" {
			src := c.alloc()
			if err := c.compileStream(x.Left, src); err != nil {
				return err
			}
			if match, ok := c.matchFilter(x.Right); ok {
				c.emit(Op{Code: OpFilterMatch, A: dst, B: src, C: match})
			} else if selectPredicate, ok := fuseSelect(x.Right); ok {
				predicate := c.subScalar(selectPredicate, "select")
				c.emit(Op{Code: OpFilterStream, A: dst, B: src, C: predicate})
			} else if steps, ok := fusePath(x.Right); ok {
				c.emit(Op{Code: OpSteps, A: dst, B: src, C: c.path(steps)})
			} else {
				right := c.subStream(x.Right, "pipe")
				c.emit(Op{Code: OpPipe, A: dst, B: src, C: right})
			}
			c.release(src)
			return nil
		}
		tmp := c.alloc()
		if err := c.compileScalar(e, tmp); err != nil {
			return err
		}
		c.emit(Op{Code: OpNode, A: dst, B: tmp})
		c.release(tmp)
		return nil
	case query.Unary:
		tmp := c.alloc()
		if err := c.compileScalar(e, tmp); err != nil {
			return err
		}
		c.emit(Op{Code: OpNode, A: dst, B: tmp})
		c.release(tmp)
		return nil
	default:
		return fmt.Errorf("bytecode: unsupported expression %T", e)
	}
}

func (c *compiler) compileScalar(e query.Expr, dst int32) error {
	switch x := e.(type) {
	case query.Literal:
		c.emit(Op{Code: OpConstValue, A: dst, B: c.prog.AddValue(x.Value)})
		return nil
	case query.Variable:
		if slot, ok := c.lookup(x.Name); ok {
			tmp := c.alloc()
			c.emit(Op{Code: OpVar, A: tmp, B: slot})
			c.emit(Op{Code: OpOne, A: dst, B: tmp})
			c.release(tmp)
		} else {
			c.emit(Op{Code: OpMissingVar, A: c.prog.AddText(x.Name)})
		}
		return nil
	case query.Path:
		tmp := c.alloc()
		if err := c.compileStream(x, tmp); err != nil {
			return err
		}
		c.emit(Op{Code: OpOne, A: dst, B: tmp})
		c.release(tmp)
		return nil
	case query.Unary:
		src := c.alloc()
		if err := c.compileScalar(x.Expr, src); err != nil {
			return err
		}
		c.emit(Op{Code: OpUnary, A: dst, B: src, C: c.prog.AddText(x.Op)})
		c.release(src)
		return nil
	case query.Binary:
		if x.Op == "|" {
			tmp := c.alloc()
			if err := c.compileStream(x, tmp); err != nil {
				return err
			}
			c.emit(Op{Code: OpOne, A: dst, B: tmp})
			c.release(tmp)
			return nil
		}
		return c.compileBinary(x, dst)
	case query.Object, query.Array, query.Call:
		tmp := c.alloc()
		if err := c.compileStream(e, tmp); err != nil {
			return err
		}
		c.emit(Op{Code: OpOne, A: dst, B: tmp})
		c.release(tmp)
		return nil
	default:
		return fmt.Errorf("bytecode: unsupported expression %T", e)
	}
}

func (c *compiler) compileBinary(b query.Binary, dst int32) error {
	switch b.Op {
	case "and", "or":
		left := c.alloc()
		if err := c.compileScalar(b.Left, left); err != nil {
			return err
		}
		var branch int32
		if b.Op == "and" {
			branch = c.jumpIf(OpJumpIfFalse, left)
		} else {
			branch = c.jumpIf(OpJumpIfTrue, left)
		}
		right := c.alloc()
		if err := c.compileScalar(b.Right, right); err != nil {
			return err
		}
		c.emit(Op{Code: OpTruth, A: dst, B: right})
		done := c.jump(OpJump, 0)
		c.patchB(branch, int32(len(c.code.Ops)))
		value := false
		if b.Op == "or" {
			value = true
		}
		c.emit(Op{Code: OpConstValue, A: dst, B: c.prog.AddValue(value)})
		c.patchA(done, int32(len(c.code.Ops)))
		c.release(left)
		c.release(right)
		return nil
	case "??":
		if err := c.compileScalar(b.Left, dst); err != nil {
			return err
		}
		notNull := c.jumpIf(OpJumpIfNotNull, dst)
		if err := c.compileScalar(b.Right, dst); err != nil {
			return err
		}
		c.patchB(notNull, int32(len(c.code.Ops)))
		return nil
	default:
		left := c.alloc()
		if err := c.compileScalar(b.Left, left); err != nil {
			return err
		}
		right := c.alloc()
		if err := c.compileScalar(b.Right, right); err != nil {
			return err
		}
		c.emit(Op{Code: OpBinary, A: dst, B: left, C: right, D: c.prog.AddText(b.Op)})
		c.release(left)
		c.release(right)
		return nil
	}
}

func (c *compiler) compileCall(call query.Call, dst int32) error {
	if err := callArityError(call); err != nil {
		c.emit(Op{Code: OpFail, A: c.prog.AddText(err.Error())})
		return nil
	}
	switch call.Name {
	case "select":
		if len(call.Args) == 1 {
			predicate := c.subScalar(call.Args[0], "select")
			c.emit(Op{Code: OpSelect, A: dst, B: predicate})
			return nil
		}
		args, err := c.callArgs(call.Args, false)
		if err != nil {
			return err
		}
		c.emit(Op{Code: OpCall, A: dst, B: c.prog.AddText(call.Name), C: c.prog.AddArgSet(args)})
		c.releaseAll(args)
		return nil
	case "walk", "path", "key":
		args := make([]int32, 0, len(call.Args))
		for _, arg := range call.Args {
			reg := c.alloc()
			if err := c.compileStream(arg, reg); err != nil {
				return err
			}
			args = append(args, reg)
		}
		c.emit(Op{Code: OpTree, A: dst, B: c.prog.AddText(call.Name), C: c.prog.AddArgSet(args)})
		c.releaseAll(args)
		return nil
	default:
		args, err := c.callArgs(call.Args, true)
		if err != nil {
			return err
		}
		c.emit(Op{Code: OpCall, A: dst, B: c.prog.AddText(call.Name), C: c.prog.AddArgSet(args)})
		c.releaseAll(args)
		return nil
	}
}

func (c *compiler) callArgs(args []query.Expr, acquire bool) ([]int32, error) {
	regs := make([]int32, 0, len(args))
	for _, arg := range args {
		reg := c.alloc()
		if acquire {
			tmp := c.alloc()
			if err := c.compileStream(arg, tmp); err != nil {
				return nil, err
			}
			c.emit(Op{Code: OpArg, A: reg, B: tmp})
			c.release(tmp)
		} else if err := c.compileStream(arg, reg); err != nil {
			return nil, err
		}
		regs = append(regs, reg)
	}
	return regs, nil
}

func (c *compiler) releaseAll(regs []int32) {
	for _, reg := range regs {
		c.release(reg)
	}
}

func (c *compiler) matchFilter(e query.Expr) (int32, bool) {
	call, ok := e.(query.Call)
	if !ok || call.Name != "select" || len(call.Args) != 1 {
		return 0, false
	}
	match, ok := c.matchPlan(call.Args[0])
	if !ok {
		return 0, false
	}
	return c.prog.AddMatch(match), true
}

func (c *compiler) matchPlan(e query.Expr) (MatchPlan, bool) {
	plan := MatchPlan{OpConst: -1, ValueConst: -1}
	if step, ok := simpleMatchPath(e); ok {
		if step.Kind == "index" {
			plan.IsIndex = true
			plan.Index = int32(step.Index)
		} else {
			plan.KeyConst = c.prog.AddText(step.Key)
		}
		return plan, true
	}
	binary, ok := e.(query.Binary)
	if !ok || !matchOperator(binary.Op) {
		return MatchPlan{}, false
	}
	step, ok := simpleMatchPath(binary.Left)
	if !ok {
		return MatchPlan{}, false
	}
	literal, ok := binary.Right.(query.Literal)
	if !ok {
		return MatchPlan{}, false
	}
	if step.Kind == "index" {
		plan.IsIndex = true
		plan.Index = int32(step.Index)
	} else {
		plan.KeyConst = c.prog.AddText(step.Key)
	}
	plan.OpConst = c.prog.AddText(binary.Op)
	plan.ValueConst = c.prog.AddValue(literal.Value)
	plan.HasValue = true
	return plan, true
}

func simpleMatchPath(e query.Expr) (query.Step, bool) {
	path, ok := e.(query.Path)
	if !ok || path.Base != nil || path.Root || len(path.Steps) != 1 {
		return query.Step{}, false
	}
	step := path.Steps[0]
	if step.Kind != "key" && step.Kind != "index" {
		return query.Step{}, false
	}
	return step, true
}

func matchOperator(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	default:
		return false
	}
}

// fusePath recognizes a right-hand pipe expression that is a path over the
// left stream. This removes a nested frame per item for chains such as
// `items[] | .name`; select predicates still use their own frame because a
// select filters a stream, not a container.
func fusePath(e query.Expr) ([]query.Step, bool) {
	switch x := e.(type) {
	case query.Path:
		if x.Base != nil || x.Root {
			return nil, false
		}
		for _, step := range x.Steps {
			switch step.Kind {
			case "key", "index":
			default:
				return nil, false
			}
		}
		return append([]query.Step(nil), x.Steps...), true
	case query.Binary:
		if x.Op != "|" {
			return nil, false
		}
		left, ok := fusePath(x.Left)
		if !ok {
			return nil, false
		}
		right, ok := fusePath(x.Right)
		if !ok {
			return nil, false
		}
		return append(left, right...), true
	default:
		return nil, false
	}
}

func fuseSelect(e query.Expr) (query.Expr, bool) {
	call, ok := e.(query.Call)
	if !ok || call.Name != "select" || len(call.Args) != 1 {
		return nil, false
	}
	return call.Args[0], true
}

func callArityError(call query.Call) error {
	switch call.Name {
	case "select":
		if len(call.Args) != 1 {
			return fmt.Errorf("select expects one predicate")
		}
	case "walk", "path", "key":
		if len(call.Args) > 1 {
			return fmt.Errorf("%s expects zero or one argument", call.Name)
		}
	case "length", "keys", "values", "type", "sort", "reverse", "first", "last":
		if len(call.Args) > 1 {
			return fmt.Errorf("%s expects zero or one argument", call.Name)
		}
	case "has", "contains", "startswith", "endswith", "test":
		if len(call.Args) != 1 && len(call.Args) != 2 {
			return fmt.Errorf("%s expects one argument (or input, argument)", call.Name)
		}
	default:
		return fmt.Errorf("unknown function %q", call.Name)
	}
	return nil
}

func statementsMutate(statements []query.Statement) bool {
	for _, statement := range statements {
		switch statement.Op {
		case "if", "for", "while":
			if statementsMutate(statement.Body) || statementsMutate(statement.Else) {
				return true
			}
		case "read", "let", "break", "continue":
		default:
			return true
		}
	}
	return false
}
