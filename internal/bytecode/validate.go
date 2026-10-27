package bytecode

import (
	"fmt"

	"github.com/vasileknik76/tql/internal/query"
	"github.com/vasileknik76/tql/syntax"
)

const (
	maxImageSize    = 64 << 20
	maxValueDepth   = 256
	maxCodeObjects  = 4096
	maxInstructions = 1_000_000
	maxConstants    = 65536
	maxPaths        = 65536
	maxTargets      = 65536
	maxSets         = 65536
	maxSlots        = 65536
	maxRegisters    = 65536
	maxPathSteps    = 256
	maxCodeDepth    = 256
	maxNameLength   = 1 << 20
)

// Validate checks structural invariants of an in-memory program. It is run by
// Decode and by Assemble; compiler-produced programs are covered by tests too.
func Validate(p *Program) error {
	if p == nil {
		return fmt.Errorf("nil program")
	}
	if len(p.Codes) == 0 || len(p.Codes) > maxCodeObjects {
		return fmt.Errorf("invalid code count %d", len(p.Codes))
	}
	if len(p.Consts) > maxConstants {
		return fmt.Errorf("too many constants")
	}
	if len(p.Paths) > maxPaths || len(p.Targets) > maxTargets {
		return fmt.Errorf("too many path or target plans")
	}
	if len(p.ArgSets) > maxSets || len(p.TargetSets) > maxSets {
		return fmt.Errorf("too many sets")
	}
	if len(p.Slots) > maxSlots {
		return fmt.Errorf("too many slots")
	}
	params := map[string]bool{}
	for _, param := range p.Params {
		if !query.ValidVariableName(param.Name) || params[param.Name] {
			return fmt.Errorf("invalid parameter name %q", param.Name)
		}
		params[param.Name] = true
		if param.HasDefault {
			if _, err := syntax.NormalizeValue(param.Default); err != nil {
				return fmt.Errorf("parameter $%s: %w", param.Name, err)
			}
		}
	}
	if len(p.Scopes) == 0 {
		return fmt.Errorf("missing root scope")
	}
	for i, scope := range p.Scopes {
		for _, slot := range scope.Slots {
			if slot < 0 || int(slot) >= len(p.Slots) {
				return fmt.Errorf("scope %d: slot %d out of range", i, slot)
			}
		}
	}
	for i, path := range p.Paths {
		if len(path.Steps) > maxPathSteps {
			return fmt.Errorf("path %d has too many steps", i)
		}
		for _, step := range path.Steps {
			if step.Kind > StepDynamic {
				return fmt.Errorf("path %d: unknown step kind", i)
			}
			if step.Kind == StepFilter || step.Kind == StepDynamic {
				if step.Code < 0 || int(step.Code) >= len(p.Codes) {
					return fmt.Errorf("path %d: code %d out of range", i, step.Code)
				}
			}
		}
	}
	for i, target := range p.Targets {
		if target.Kind != TargetPath && target.Kind != TargetExpr {
			return fmt.Errorf("target %d: unknown kind", i)
		}
		if target.Base < -1 || (target.Base >= 0 && int(target.Base) >= len(p.Codes)) {
			return fmt.Errorf("target %d: base code out of range", i)
		}
		if target.Kind == TargetExpr && target.Base < 0 {
			return fmt.Errorf("target %d: expression target has no code", i)
		}
		if target.Kind == TargetPath && (target.Path < 0 || int(target.Path) >= len(p.Paths)) {
			return fmt.Errorf("target %d: path out of range", i)
		}
	}
	for i, loop := range p.Loops {
		if loop.Kind != LoopWhile && loop.Kind != LoopFor {
			return fmt.Errorf("loop %d: unknown kind", i)
		}
		if loop.Continue < 0 || loop.Break < 0 {
			return fmt.Errorf("loop %d: missing continue or break target", i)
		}
		if loop.Kind == LoopFor {
			if loop.Scope < 0 || int(loop.Scope) >= len(p.Scopes) {
				return fmt.Errorf("loop %d: scope out of range", i)
			}
			if loop.VarSlot < 0 || int(loop.VarSlot) >= len(p.Slots) {
				return fmt.Errorf("loop %d: variable slot out of range", i)
			}
		}
	}
	totalOps := 0
	for ci, code := range p.Codes {
		if code == nil {
			return fmt.Errorf("code %d is nil", ci)
		}
		if code.Registers < 0 || code.Registers > maxRegisters {
			return fmt.Errorf("code %d: invalid register count", ci)
		}
		totalOps += len(code.Ops)
		if totalOps > maxInstructions {
			return fmt.Errorf("too many instructions")
		}
		if len(code.Ops) == 0 || code.Ops[len(code.Ops)-1].Code != OpHalt {
			return fmt.Errorf("code %d does not end in HALT", ci)
		}
		for oi, op := range code.Ops {
			if !op.Code.Valid() {
				return fmt.Errorf("code %d op %d: unknown opcode %d", ci, oi, op.Code)
			}
			if op.Stmt < 0 {
				return fmt.Errorf("code %d op %d: negative statement index", ci, oi)
			}
			if err := validateOp(p, ci, oi, code, op); err != nil {
				return err
			}
		}
	}
	if err := validateCodeGraph(p); err != nil {
		return err
	}
	return nil
}

func validateOp(p *Program, ci, oi int, code *Code, op Op) error {
	reg := func(index int32) error {
		if index < 0 || int(index) >= code.Registers {
			return fmt.Errorf("code %d op %d: register %d out of range", ci, oi, index)
		}
		return nil
	}
	konst := func(index int32) error {
		if index < 0 || int(index) >= len(p.Consts) {
			return fmt.Errorf("code %d op %d: constant %d out of range", ci, oi, index)
		}
		return nil
	}
	path := func(index int32) error {
		if index < 0 || int(index) >= len(p.Paths) {
			return fmt.Errorf("code %d op %d: path %d out of range", ci, oi, index)
		}
		return nil
	}
	sub := func(index int32) error {
		if index < 0 || int(index) >= len(p.Codes) {
			return fmt.Errorf("code %d op %d: code %d out of range", ci, oi, index)
		}
		return nil
	}
	target := func(index int32) error {
		if index < 0 || int(index) >= len(p.Targets) {
			return fmt.Errorf("code %d op %d: target %d out of range", ci, oi, index)
		}
		return nil
	}
	jump := func(index int32) error {
		if index < 0 || int(index) >= len(code.Ops) {
			return fmt.Errorf("code %d op %d: jump target %d out of range", ci, oi, index)
		}
		return nil
	}
	argSet := func(index int32) error {
		if index < 0 || int(index) >= len(p.ArgSets) {
			return fmt.Errorf("code %d op %d: argument set %d out of range", ci, oi, index)
		}
		for _, item := range p.ArgSets[index] {
			if item < 0 || int(item) >= code.Registers {
				return fmt.Errorf("code %d op %d: argument register %d out of range", ci, oi, item)
			}
		}
		return nil
	}
	switch op.Code {
	case OpConst:
		if err := reg(op.A); err != nil {
			return err
		}
		return konst(op.B)
	case OpConstValue:
		if err := reg(op.A); err != nil {
			return err
		}
		return konst(op.B)
	case OpCtx, OpRoot:
		return reg(op.A)
	case OpVar:
		if err := reg(op.A); err != nil {
			return err
		}
		if op.B < 0 || int(op.B) >= len(p.Slots) {
			return fmt.Errorf("code %d op %d: slot %d out of range", ci, oi, op.B)
		}
		return nil
	case OpMissingVar, OpFail:
		return konst(op.A)
	case OpOne, OpArg, OpNode:
		if err := reg(op.A); err != nil {
			return err
		}
		return reg(op.B)
	case OpTruth:
		if err := reg(op.A); err != nil {
			return err
		}
		return reg(op.B)
	case OpSteps:
		if err := reg(op.A); err != nil {
			return err
		}
		if err := reg(op.B); err != nil {
			return err
		}
		if err := path(op.C); err != nil {
			return err
		}
		for _, step := range p.Paths[op.C].Steps {
			if step.Kind == StepFilter || step.Kind == StepDynamic {
				if err := sub(step.Code); err != nil {
					return err
				}
			}
		}
		return nil
	case OpObject:
		if err := reg(op.A); err != nil {
			return err
		}
		if err := konst(op.B); err != nil {
			return err
		}
		if err := argSet(op.C); err != nil {
			return err
		}
		if len(p.Consts[op.B].Texts) != len(p.ArgSets[op.C]) {
			return fmt.Errorf("code %d op %d: object keys and values differ", ci, oi)
		}
		return nil
	case OpArray:
		if err := reg(op.A); err != nil {
			return err
		}
		return argSet(op.B)
	case OpUnary:
		if err := reg(op.A); err != nil {
			return err
		}
		if err := reg(op.B); err != nil {
			return err
		}
		if err := konst(op.C); err != nil {
			return err
		}
		switch p.Consts[op.C].Text {
		case "+", "-", "not", "!":
			return nil
		default:
			return fmt.Errorf("code %d op %d: unknown unary operator %q", ci, oi, p.Consts[op.C].Text)
		}
	case OpBinary:
		if err := reg(op.A); err != nil {
			return err
		}
		if err := reg(op.B); err != nil {
			return err
		}
		if err := reg(op.C); err != nil {
			return err
		}
		if err := konst(op.D); err != nil {
			return err
		}
		switch p.Consts[op.D].Text {
		case "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=", "and", "or", "??":
			return nil
		default:
			return fmt.Errorf("code %d op %d: unknown binary operator %q", ci, oi, p.Consts[op.D].Text)
		}
	case OpJump:
		return jump(op.A)
	case OpJumpIfFalse, OpJumpIfTrue, OpJumpIfNotNull:
		if err := reg(op.A); err != nil {
			return err
		}
		return jump(op.B)
	case OpPipe:
		if err := reg(op.A); err != nil {
			return err
		}
		if err := reg(op.B); err != nil {
			return err
		}
		return sub(op.C)
	case OpCall:
		if err := reg(op.A); err != nil {
			return err
		}
		if err := konst(op.B); err != nil {
			return err
		}
		return argSet(op.C)
	case OpSelect:
		if err := reg(op.A); err != nil {
			return err
		}
		return sub(op.B)
	case OpFilterStream:
		if err := reg(op.A); err != nil {
			return err
		}
		if err := reg(op.B); err != nil {
			return err
		}
		return sub(op.C)
	case OpTree:
		if err := reg(op.A); err != nil {
			return err
		}
		if err := konst(op.B); err != nil {
			return err
		}
		switch p.Consts[op.B].Text {
		case "walk", "path", "key":
		default:
			return fmt.Errorf("code %d op %d: unknown tree function %q", ci, oi, p.Consts[op.B].Text)
		}
		return argSet(op.C)
	case OpTick:
		return nil
	case OpRead:
		return reg(op.A)
	case OpCheckDeclare:
		return konst(op.A)
	case OpDeclare:
		if op.A < 0 || int(op.A) >= len(p.Slots) {
			return fmt.Errorf("code %d op %d: slot %d out of range", ci, oi, op.A)
		}
		return reg(op.B)
	case OpAssign:
		if err := konst(op.A); err != nil {
			return err
		}
		if err := assignOp(p.Consts[op.A].Text); err != nil {
			return fmt.Errorf("code %d op %d: %w", ci, oi, err)
		}
		if err := target(op.B); err != nil {
			return err
		}
		return sub(op.C)
	case OpAssignConst:
		if err := konst(op.A); err != nil {
			return err
		}
		if err := assignOp(p.Consts[op.A].Text); err != nil {
			return fmt.Errorf("code %d op %d: %w", ci, oi, err)
		}
		if err := target(op.B); err != nil {
			return err
		}
		return konst(op.C)
	case OpDelete:
		if op.A < 0 || int(op.A) >= len(p.TargetSets) {
			return fmt.Errorf("code %d op %d: target set %d out of range", ci, oi, op.A)
		}
		for _, item := range p.TargetSets[op.A] {
			if err := sub(item); err != nil {
				return err
			}
		}
		return nil
	case OpForInit:
		if op.A < 0 || int(op.A) >= len(p.Loops) {
			return fmt.Errorf("code %d op %d: loop %d out of range", ci, oi, op.A)
		}
		return sub(op.B)
	case OpLoopEnter:
		if op.A < 0 || int(op.A) >= len(p.Loops) {
			return fmt.Errorf("code %d op %d: loop %d out of range", ci, oi, op.A)
		}
		return nil
	case OpForNext:
		if op.A < 0 || int(op.A) >= len(p.Loops) {
			return fmt.Errorf("code %d op %d: loop %d out of range", ci, oi, op.A)
		}
		return jump(op.B)
	case OpForClose:
		if op.A < 0 || int(op.A) >= len(p.Loops) {
			return fmt.Errorf("code %d op %d: loop %d out of range", ci, oi, op.A)
		}
		return nil
	case OpLoopBreak, OpLoopContinue:
		if op.A < 0 || int(op.A) >= len(p.Loops) {
			return fmt.Errorf("code %d op %d: loop %d out of range", ci, oi, op.A)
		}
		target := p.Loops[op.A].Break
		if op.Code == OpLoopContinue {
			target = p.Loops[op.A].Continue
		}
		if target < 0 || int(target) >= len(code.Ops) {
			return fmt.Errorf("code %d op %d: loop target %d out of range", ci, oi, target)
		}
		return nil
	case OpEnterScope, OpExitScope:
		if op.Code == OpEnterScope && (op.A < 0 || int(op.A) >= len(p.Scopes)) {
			return fmt.Errorf("code %d op %d: scope %d out of range", ci, oi, op.A)
		}
		return nil
	case OpStmtEnter, OpStmtLeave, OpHalt:
		return nil
	default:
		return fmt.Errorf("code %d op %d: unhandled opcode %s", ci, oi, op.Code)
	}
}

func validateCodeGraph(p *Program) error {
	edges := make([][]int32, len(p.Codes))
	add := func(from int, to int32) {
		if to >= 0 && int(to) < len(p.Codes) {
			edges[from] = append(edges[from], to)
		}
	}
	for ci, code := range p.Codes {
		for _, op := range code.Ops {
			switch op.Code {
			case OpPipe, OpSelect, OpAssign, OpFilterStream:
				add(ci, op.C)
			case OpForInit:
				add(ci, op.B)
			case OpSteps:
				for _, step := range p.Paths[op.C].Steps {
					if step.Kind == StepFilter || step.Kind == StepDynamic {
						add(ci, step.Code)
					}
				}
			}
		}
	}
	for _, path := range p.Paths {
		for _, step := range path.Steps {
			if step.Kind == StepFilter || step.Kind == StepDynamic {
				add(0, step.Code)
			}
		}
	}
	for _, target := range p.Targets {
		add(0, target.Base)
	}
	for _, set := range p.TargetSets {
		for _, code := range set {
			add(0, code)
		}
	}
	state := make([]uint8, len(p.Codes))
	var visit func(int, int) error
	visit = func(node, depth int) error {
		if depth > maxCodeDepth {
			return fmt.Errorf("sub-code depth exceeds %d", maxCodeDepth)
		}
		switch state[node] {
		case 1:
			return fmt.Errorf("cyclic sub-code graph at code %d", node)
		case 2:
			return nil
		}
		state[node] = 1
		for _, next := range edges[node] {
			if err := visit(int(next), depth+1); err != nil {
				return err
			}
		}
		state[node] = 2
		return nil
	}
	return visit(0, 0)
}

// DeriveMutates reports whether the code contains an assignment or deletion.
func DeriveMutates(p *Program) bool {
	for _, code := range p.Codes {
		for _, op := range code.Ops {
			if op.Code == OpAssign || op.Code == OpAssignConst || op.Code == OpDelete {
				return true
			}
		}
	}
	return false
}

func assignOp(op string) error {
	switch op {
	case "=", "|=", "+=", "-=", "*=", "/=", "??=":
		return nil
	default:
		return fmt.Errorf("unknown assignment operator %q", op)
	}
}
