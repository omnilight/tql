package bytecode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

// Disassemble returns canonical, re-assemblable text. Lines beginning with #
// are comments and are ignored by Assemble.
func Disassemble(p *Program) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# TQL bytecode v%d\n", FormatVersion)
	fmt.Fprintf(&b, ".version %d\n", FormatVersion)
	for i, param := range p.Params {
		if param.HasDefault {
			fmt.Fprintf(&b, ".param %s default %s\n", syntax.Quote(param.Name), formatValue(param.Default))
		} else {
			fmt.Fprintf(&b, ".param %s required\n", syntax.Quote(param.Name))
		}
		_ = i
	}
	for i, c := range p.Consts {
		switch {
		case c.Texts != nil:
			fmt.Fprintf(&b, ".const K%d texts %s\n", i, formatStrings(c.Texts))
		case c.TextSet || c.Text != "":
			fmt.Fprintf(&b, ".const K%d text %s\n", i, syntax.Quote(c.Text))
		default:
			fmt.Fprintf(&b, ".const K%d json %s\n", i, formatJSON(c.Value))
		}
	}
	for i, slot := range p.Slots {
		fmt.Fprintf(&b, ".slot S%d %s\n", i, syntax.Quote(slot))
	}
	for i, match := range p.Matches {
		selector := fmt.Sprintf("key=K%d", match.KeyConst)
		if match.IsIndex {
			selector = fmt.Sprintf("index=%d", match.Index)
		}
		if match.HasValue {
			fmt.Fprintf(&b, ".match M%d %s op=K%d value=K%d\n", i, selector, match.OpConst, match.ValueConst)
		} else {
			fmt.Fprintf(&b, ".match M%d %s truth\n", i, selector)
		}
	}
	for i, path := range p.Paths {
		fmt.Fprintf(&b, ".path P%d\n", i)
		for _, step := range path.Steps {
			switch step.Kind {
			case StepKey:
				fmt.Fprintf(&b, ".step key %s\n", syntax.Quote(step.Key))
			case StepIndex:
				fmt.Fprintf(&b, ".step index %d\n", step.Index)
			case StepWildcard:
				fmt.Fprintf(&b, ".step wildcard\n")
			case StepSlice:
				from, to := "*", "*"
				if step.HasFrom {
					from = strconv.Itoa(step.From)
				}
				if step.HasTo {
					to = strconv.Itoa(step.To)
				}
				fmt.Fprintf(&b, ".step slice %s %s\n", from, to)
			case StepFilter:
				fmt.Fprintf(&b, ".step filter C%d\n", step.Code)
			case StepDynamic:
				fmt.Fprintf(&b, ".step dynamic C%d\n", step.Code)
			case StepMatch:
				fmt.Fprintf(&b, ".step match M%d\n", step.Code)
			}
		}
	}
	for i, target := range p.Targets {
		if target.Kind == TargetPath {
			fmt.Fprintf(&b, ".target T%d path root=%t base=%d path=P%d\n", i, target.Root, target.Base, target.Path)
		} else {
			fmt.Fprintf(&b, ".target T%d expr C%d\n", i, target.Base)
		}
	}
	for i, set := range p.ArgSets {
		fmt.Fprintf(&b, ".argset A%d", i)
		for _, reg := range set {
			fmt.Fprintf(&b, " %d", reg)
		}
		fmt.Fprintln(&b)
	}
	for i, set := range p.TargetSets {
		fmt.Fprintf(&b, ".targetset D%d", i)
		for _, code := range set {
			fmt.Fprintf(&b, " C%d", code)
		}
		fmt.Fprintln(&b)
	}
	for i, scope := range p.Scopes {
		fmt.Fprintf(&b, ".scope S%d", i)
		for _, slot := range scope.Slots {
			fmt.Fprintf(&b, " %d", slot)
		}
		fmt.Fprintln(&b)
	}
	for i, loop := range p.Loops {
		kind := "while"
		if loop.Kind == LoopFor {
			kind = "for"
		}
		fmt.Fprintf(&b, ".loop L%d %s continue=%d break=%d scope=%d varslot=%d readonly=%t\n", i, kind, loop.Continue, loop.Break, loop.Scope, loop.VarSlot, loop.ReadOnly)
	}
	for i, code := range p.Codes {
		fmt.Fprintf(&b, ".code C%d %s %d\n", i, syntax.Quote(code.Name), code.Registers)
		lastStmt := int32(-1)
		for _, op := range code.Ops {
			if op.Stmt != lastStmt {
				fmt.Fprintf(&b, ".stmt %d\n", op.Stmt)
				lastStmt = op.Stmt
			}
			fmt.Fprintln(&b, formatOp(op))
		}
	}
	return b.String()
}

func formatOp(op Op) string {
	name := op.Code.String()
	switch op.Code {
	case OpConst:
		return fmt.Sprintf("%s %d K%d", name, op.A, op.B)
	case OpConstValue:
		return fmt.Sprintf("%s %d K%d", name, op.A, op.B)
	case OpCtx, OpRoot:
		return fmt.Sprintf("%s %d", name, op.A)
	case OpVar:
		return fmt.Sprintf("%s %d S%d", name, op.A, op.B)
	case OpMissingVar, OpFail:
		return fmt.Sprintf("%s K%d", name, op.A)
	case OpOne, OpArg, OpNode, OpTruth, OpAnyTruth:
		return fmt.Sprintf("%s %d %d", name, op.A, op.B)
	case OpSteps:
		return fmt.Sprintf("%s %d %d P%d", name, op.A, op.B, op.C)
	case OpObject:
		return fmt.Sprintf("%s %d K%d A%d", name, op.A, op.B, op.C)
	case OpArray:
		return fmt.Sprintf("%s %d A%d", name, op.A, op.B)
	case OpUnary:
		return fmt.Sprintf("%s %d %d K%d", name, op.A, op.B, op.C)
	case OpBinary:
		return fmt.Sprintf("%s %d %d %d K%d", name, op.A, op.B, op.C, op.D)
	case OpJump:
		return fmt.Sprintf("%s %d", name, op.A)
	case OpJumpIfFalse, OpJumpIfTrue, OpJumpIfNotNull:
		return fmt.Sprintf("%s %d %d", name, op.A, op.B)
	case OpPipe:
		return fmt.Sprintf("%s %d %d C%d", name, op.A, op.B, op.C)
	case OpFilterStream:
		return fmt.Sprintf("%s %d %d C%d", name, op.A, op.B, op.C)
	case OpFilterMatch:
		return fmt.Sprintf("%s %d %d M%d", name, op.A, op.B, op.C)
	case OpCall:
		return fmt.Sprintf("%s %d K%d A%d", name, op.A, op.B, op.C)
	case OpSelect:
		return fmt.Sprintf("%s %d C%d", name, op.A, op.B)
	case OpTree:
		return fmt.Sprintf("%s %d K%d A%d", name, op.A, op.B, op.C)
	case OpTick, OpHalt, OpStmtLeave, OpExitScope:
		return name
	case OpRead, OpReadValue:
		return fmt.Sprintf("%s %d", name, op.A)
	case OpCheckDeclare:
		return fmt.Sprintf("%s K%d", name, op.A)
	case OpDeclare:
		return fmt.Sprintf("%s S%d %d", name, op.A, op.B)
	case OpAssign:
		return fmt.Sprintf("%s K%d T%d C%d", name, op.A, op.B, op.C)
	case OpAssignConst:
		return fmt.Sprintf("%s K%d T%d K%d", name, op.A, op.B, op.C)
	case OpDelete:
		return fmt.Sprintf("%s D%d", name, op.A)
	case OpForInit:
		return fmt.Sprintf("%s L%d C%d", name, op.A, op.B)
	case OpLoopEnter, OpForClose, OpLoopBreak, OpLoopContinue:
		return fmt.Sprintf("%s L%d", name, op.A)
	case OpForNext:
		return fmt.Sprintf("%s L%d %d", name, op.A, op.B)
	case OpEnterScope:
		return fmt.Sprintf("%s S%d", name, op.A)
	case OpStmtEnter:
		return fmt.Sprintf("%s %d", name, op.A)
	default:
		return name
	}
}

func formatValue(value any) string {
	if temporal, ok := value.(syntax.Temporal); ok {
		return "datetime " + syntax.Quote(string(temporal))
	}
	return "json " + formatJSON(value)
}

func formatJSON(value any) string {
	s, err := syntax.JSON(value)
	if err != nil {
		return "null"
	}
	return s
}

func formatStrings(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = syntax.Quote(value)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
