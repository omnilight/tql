package bytecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/vasileknik76/tql/syntax"
)

// Assemble parses canonical assembly into a validated Program.
func Assemble(text string) (result *Program, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("assembly error: %v", recovered)
		}
	}()
	p := &Program{}
	state := &assembler{prog: p, currentPath: -1, currentCode: -1}
	scanner := bufio.NewScanner(strings.NewReader(text))
	// Assembly lines with long constants can exceed Scanner's default token size.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		state.line++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := state.parseLine(line); err != nil {
			return nil, fmt.Errorf("assembly line %d: %w", state.line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	state.finish()
	p.Mutates = DeriveMutates(p)
	if err := Validate(p); err != nil {
		return nil, fmt.Errorf("invalid assembly: %w", err)
	}
	return p, nil
}

type assembler struct {
	prog        *Program
	line        int
	currentPath int
	currentCode int
	stmt        int32
}

func (a *assembler) finish() {
	a.currentPath = -1
	a.currentCode = -1
}

func (a *assembler) parseLine(line string) error {
	if strings.HasPrefix(line, ".") {
		return a.directive(line)
	}
	if a.currentCode < 0 {
		return fmt.Errorf("instruction outside .code")
	}
	op, err := parseOp(line, a.stmt)
	if err != nil {
		return err
	}
	code := a.prog.Codes[a.currentCode]
	code.Ops = append(code.Ops, op)
	return nil
}

func (a *assembler) directive(line string) error {
	fields := strings.Fields(line)
	name := fields[0]
	args := fields[1:]
	switch name {
	case ".version":
		if len(args) != 1 {
			return fmt.Errorf(".version expects one argument")
		}
		v, err := strconv.Atoi(args[0])
		if err != nil || v != FormatVersion {
			return fmt.Errorf("unsupported assembly version %q", args[0])
		}
		return nil
	case ".param":
		name, rest, err := splitQuoted(line)
		if err != nil {
			return err
		}
		param := Param{Name: name}
		if rest == "required" {
			param.HasDefault = false
		} else if strings.HasPrefix(rest, "default ") {
			param.HasDefault = true
			param.Default, err = parseValue(strings.TrimPrefix(rest, "default "))
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf(".param expects required or default")
		}
		a.prog.Params = append(a.prog.Params, param)
		return nil
	case ".const":
		if len(args) < 2 {
			return fmt.Errorf(".const expects an ID and a kind")
		}
		id, err := parseID(args[0], 'K')
		if err != nil {
			return err
		}
		if id != int32(len(a.prog.Consts)) {
			return fmt.Errorf("constant IDs must be contiguous")
		}
		c := Const{}
		switch args[1] {
		case "json":
			c.Value, err = parseValue("json " + strings.TrimSpace(line[strings.Index(line, "json")+4:]))
		case "text":
			c.Text, err = parseQuoted(strings.TrimSpace(line[strings.Index(line, "text")+4:]))
			c.TextSet = true
		case "texts":
			var texts []string
			err = json.Unmarshal([]byte(strings.TrimSpace(line[strings.Index(line, "texts")+5:])), &texts)
			c.Texts = texts
			if c.Texts == nil {
				c.Texts = []string{}
			}
		default:
			return fmt.Errorf("unknown constant kind %q", args[1])
		}
		if err != nil {
			return err
		}
		a.prog.Consts = append(a.prog.Consts, c)
		return nil
	case ".slot":
		if len(args) != 2 {
			return fmt.Errorf(".slot expects an ID and a name")
		}
		id, err := parseID(args[0], 'S')
		if err != nil {
			return err
		}
		if id != int32(len(a.prog.Slots)) {
			return fmt.Errorf("slot IDs must be contiguous")
		}
		name, err := parseQuoted(strings.TrimSpace(line[strings.Index(line, args[0])+len(args[0]):]))
		if err != nil {
			return err
		}
		a.prog.Slots = append(a.prog.Slots, name)
		return nil
	case ".match":
		if len(args) < 2 {
			return fmt.Errorf(".match expects an ID and a selector")
		}
		id, err := parseID(args[0], 'M')
		if err != nil {
			return err
		}
		if id != int32(len(a.prog.Matches)) {
			return fmt.Errorf("match IDs must be contiguous")
		}
		match, err := parseMatch(args)
		if err != nil {
			return err
		}
		a.prog.Matches = append(a.prog.Matches, match)
		return nil
	case ".path":
		if len(args) != 1 {
			return fmt.Errorf(".path expects one ID")
		}
		id, err := parseID(args[0], 'P')
		if err != nil {
			return err
		}
		if id != int32(len(a.prog.Paths)) {
			return fmt.Errorf("path IDs must be contiguous")
		}
		a.prog.Paths = append(a.prog.Paths, PathPlan{})
		a.currentPath = int(id)
		return nil
	case ".step":
		if a.currentPath < 0 {
			return fmt.Errorf(".step outside .path")
		}
		step, err := parseStep(line)
		if err != nil {
			return err
		}
		a.prog.Paths[a.currentPath].Steps = append(a.prog.Paths[a.currentPath].Steps, step)
		return nil
	case ".target":
		if len(args) < 2 {
			return fmt.Errorf(".target expects an ID and a kind")
		}
		id, err := parseID(args[0], 'T')
		if err != nil {
			return err
		}
		if id != int32(len(a.prog.Targets)) {
			return fmt.Errorf("target IDs must be contiguous")
		}
		target, err := parseTarget(line)
		if err != nil {
			return err
		}
		a.prog.Targets = append(a.prog.Targets, target)
		return nil
	case ".argset":
		if len(args) < 1 {
			return fmt.Errorf(".argset expects an ID")
		}
		id, err := parseID(args[0], 'A')
		if err != nil {
			return err
		}
		if id != int32(len(a.prog.ArgSets)) {
			return fmt.Errorf("argset IDs must be contiguous")
		}
		set := make([]int32, 0, len(args)-1)
		for _, token := range args[1:] {
			reg, err := strconv.Atoi(token)
			if err != nil {
				return fmt.Errorf("invalid register %q", token)
			}
			set = append(set, int32(reg))
		}
		a.prog.ArgSets = append(a.prog.ArgSets, set)
		return nil
	case ".targetset":
		if len(args) < 1 {
			return fmt.Errorf(".targetset expects an ID")
		}
		id, err := parseID(args[0], 'D')
		if err != nil {
			return err
		}
		if id != int32(len(a.prog.TargetSets)) {
			return fmt.Errorf("targetset IDs must be contiguous")
		}
		set := make([]int32, 0, len(args)-1)
		for _, token := range args[1:] {
			code, err := parseID(token, 'C')
			if err != nil {
				return err
			}
			set = append(set, code)
		}
		a.prog.TargetSets = append(a.prog.TargetSets, set)
		return nil
	case ".scope":
		if len(args) < 1 {
			return fmt.Errorf(".scope expects an ID")
		}
		id, err := parseID(args[0], 'S')
		if err != nil {
			return err
		}
		if id != int32(len(a.prog.Scopes)) {
			return fmt.Errorf("scope IDs must be contiguous")
		}
		scope := Scope{}
		for _, token := range args[1:] {
			slot, err := strconv.Atoi(token)
			if err != nil {
				return fmt.Errorf("invalid slot %q", token)
			}
			scope.Slots = append(scope.Slots, int32(slot))
		}
		a.prog.Scopes = append(a.prog.Scopes, scope)
		return nil
	case ".loop":
		if len(args) < 1 {
			return fmt.Errorf(".loop expects an ID")
		}
		id, err := parseID(args[0], 'L')
		if err != nil {
			return err
		}
		if id != int32(len(a.prog.Loops)) {
			return fmt.Errorf("loop IDs must be contiguous")
		}
		loop, err := parseLoop(args)
		if err != nil {
			return err
		}
		a.prog.Loops = append(a.prog.Loops, loop)
		return nil
	case ".code":
		if len(args) < 3 {
			return fmt.Errorf(".code expects an ID, a name and a register count")
		}
		id, err := parseID(args[0], 'C')
		if err != nil {
			return err
		}
		if id != int32(len(a.prog.Codes)) {
			return fmt.Errorf("code IDs must be contiguous")
		}
		name, err := parseQuoted(args[1])
		if err != nil {
			return err
		}
		registers, err := strconv.Atoi(args[2])
		if err != nil || registers < 0 {
			return fmt.Errorf("invalid register count %q", args[2])
		}
		a.prog.Codes = append(a.prog.Codes, &Code{Name: name, Registers: registers})
		a.currentCode = int(id)
		a.stmt = 0
		return nil
	case ".stmt":
		if len(args) != 1 {
			return fmt.Errorf(".stmt expects one number")
		}
		value, err := strconv.Atoi(args[0])
		if err != nil || value < 0 {
			return fmt.Errorf("invalid statement index %q", args[0])
		}
		a.stmt = int32(value)
		return nil
	case ".end":
		a.currentPath = -1
		a.currentCode = -1
		return nil
	default:
		return fmt.Errorf("unknown directive %q", name)
	}
}

func parseStep(line string) (Step, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return Step{}, fmt.Errorf(".step expects a kind")
	}
	step := Step{}
	switch fields[1] {
	case "key":
		if len(fields) != 3 {
			return Step{}, fmt.Errorf("key step expects a quoted key")
		}
		key, err := parseQuoted(fields[2])
		if err != nil {
			return Step{}, err
		}
		step.Kind = StepKey
		step.Key = key
	case "index":
		if len(fields) != 3 {
			return Step{}, fmt.Errorf("index step expects one number")
		}
		value, err := strconv.Atoi(fields[2])
		if err != nil {
			return Step{}, err
		}
		step.Kind = StepIndex
		step.Index = value
	case "wildcard":
		if len(fields) != 2 {
			return Step{}, fmt.Errorf("wildcard step takes no argument")
		}
		step.Kind = StepWildcard
	case "slice":
		if len(fields) != 4 {
			return Step{}, fmt.Errorf("slice step expects from and to")
		}
		step.Kind = StepSlice
		if fields[2] != "*" {
			value, err := strconv.Atoi(fields[2])
			if err != nil {
				return Step{}, err
			}
			step.HasFrom = true
			step.From = value
		}
		if fields[3] != "*" {
			value, err := strconv.Atoi(fields[3])
			if err != nil {
				return Step{}, err
			}
			step.HasTo = true
			step.To = value
		}
	case "filter", "dynamic":
		if len(fields) != 3 {
			return Step{}, fmt.Errorf("%s step expects a code ID", fields[1])
		}
		code, err := parseID(fields[2], 'C')
		if err != nil {
			return Step{}, err
		}
		if fields[1] == "filter" {
			step.Kind = StepFilter
		} else {
			step.Kind = StepDynamic
		}
		step.Code = code
	case "match":
		if len(fields) != 3 {
			return Step{}, fmt.Errorf("match step expects a match ID")
		}
		match, err := parseID(fields[2], 'M')
		if err != nil {
			return Step{}, err
		}
		step.Kind = StepMatch
		step.Code = match
	default:
		return Step{}, fmt.Errorf("unknown step kind %q", fields[1])
	}
	return step, nil
}

func parseTarget(line string) (TargetPlan, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return TargetPlan{}, fmt.Errorf(".target expects an ID and a kind")
	}
	id, err := parseID(fields[1], 'T')
	if err != nil {
		return TargetPlan{}, err
	}
	_ = id
	target := TargetPlan{Base: -1, Path: -1}
	switch fields[2] {
	case "path":
		target.Kind = TargetPath
		for _, field := range fields[3:] {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 {
				return TargetPlan{}, fmt.Errorf("invalid target field %q", field)
			}
			switch parts[0] {
			case "root":
				target.Root = parts[1] == "true"
			case "base":
				value, err := strconv.Atoi(parts[1])
				if err != nil {
					return TargetPlan{}, err
				}
				target.Base = int32(value)
			case "path":
				value, err := parseID(parts[1], 'P')
				if err != nil {
					return TargetPlan{}, err
				}
				target.Path = value
			default:
				return TargetPlan{}, fmt.Errorf("unknown target field %q", parts[0])
			}
		}
		if target.Path < 0 {
			return TargetPlan{}, fmt.Errorf("path target is missing path=Pn")
		}
	case "expr":
		if len(fields) != 4 {
			return TargetPlan{}, fmt.Errorf("expr target expects a code ID")
		}
		code, err := parseID(fields[3], 'C')
		if err != nil {
			return TargetPlan{}, err
		}
		target.Kind = TargetExpr
		target.Base = code
	default:
		return TargetPlan{}, fmt.Errorf("unknown target kind %q", fields[2])
	}
	return target, nil
}

func parseMatch(args []string) (MatchPlan, error) {
	if len(args) < 2 {
		return MatchPlan{}, fmt.Errorf(".match expects an ID and a selector")
	}
	id, err := parseID(args[0], 'M')
	if err != nil {
		return MatchPlan{}, err
	}
	_ = id
	match := MatchPlan{OpConst: -1, ValueConst: -1}
	for _, field := range args[1:] {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) == 1 {
			if parts[0] == "truth" {
				continue
			}
			return MatchPlan{}, fmt.Errorf("invalid match field %q", field)
		}
		switch parts[0] {
		case "key":
			match.KeyConst, err = parseID(parts[1], 'K')
			if err != nil {
				return MatchPlan{}, err
			}
		case "index":
			value, e := strconv.Atoi(parts[1])
			if e != nil {
				return MatchPlan{}, e
			}
			if !fitsInt32(value) {
				return MatchPlan{}, fmt.Errorf("match index %d is out of range", value)
			}
			match.Index = int32(value)
			match.IsIndex = true
		case "op":
			match.OpConst, err = parseID(parts[1], 'K')
			if err != nil {
				return MatchPlan{}, err
			}
			match.HasValue = true
		case "value":
			match.ValueConst, err = parseID(parts[1], 'K')
			if err != nil {
				return MatchPlan{}, err
			}
			match.HasValue = true
		default:
			return MatchPlan{}, fmt.Errorf("unknown match field %q", parts[0])
		}
	}
	if !match.IsIndex && match.KeyConst < 0 {
		return MatchPlan{}, fmt.Errorf("match needs key or index")
	}
	if match.HasValue && (match.OpConst < 0 || match.ValueConst < 0) {
		return MatchPlan{}, fmt.Errorf("match comparison needs op and value")
	}
	return match, nil
}

func parseLoop(args []string) (Loop, error) {
	loop := Loop{Continue: -1, Break: -1, Scope: -1, VarSlot: -1}
	if len(args) < 2 {
		return loop, fmt.Errorf("loop expects a kind")
	}
	switch args[1] {
	case "while":
		loop.Kind = LoopWhile
	case "for":
		loop.Kind = LoopFor
	default:
		return loop, fmt.Errorf("unknown loop kind %q", args[1])
	}
	for _, field := range args[2:] {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			return loop, fmt.Errorf("invalid loop field %q", field)
		}
		switch parts[0] {
		case "continue":
			value, err := strconv.Atoi(parts[1])
			if err != nil {
				return loop, err
			}
			loop.Continue = int32(value)
		case "break":
			value, err := strconv.Atoi(parts[1])
			if err != nil {
				return loop, err
			}
			loop.Break = int32(value)
		case "scope":
			value, err := parseID(parts[1], 'S')
			if err != nil {
				n, e := strconv.Atoi(parts[1])
				if e != nil {
					return loop, err
				}
				value = int32(n)
			}
			loop.Scope = value
		case "varslot":
			value, err := parseID(parts[1], 'S')
			if err != nil {
				// The disassembler prints a numeric slot, not a scope ID.
				n, e := strconv.Atoi(parts[1])
				if e != nil {
					return loop, err
				}
				value = int32(n)
			}
			loop.VarSlot = value
		case "readonly":
			loop.ReadOnly = parts[1] == "true"
		default:
			return loop, fmt.Errorf("unknown loop field %q", parts[0])
		}
	}
	return loop, nil
}

func parseOp(line string, stmt int32) (Op, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Op{}, fmt.Errorf("empty instruction")
	}
	op := Op{Stmt: stmt}
	var err error
	reg := func(index int) (int32, error) {
		if index >= len(fields) {
			return 0, fmt.Errorf("%s is missing register %d", fields[0], index)
		}
		value, e := strconv.Atoi(fields[index])
		if e != nil {
			return 0, fmt.Errorf("invalid register %q", fields[index])
		}
		return int32(value), nil
	}
	konst := func(index int) (int32, error) {
		if index >= len(fields) {
			return 0, fmt.Errorf("%s is missing constant", fields[0])
		}
		return parseID(fields[index], 'K')
	}
	switch fields[0] {
	case "CONST":
		op.Code = OpConst
		op.A, err = reg(1)
		if err == nil {
			op.B, err = konst(2)
		}
	case "CONST_VALUE":
		op.Code = OpConstValue
		op.A, err = reg(1)
		if err == nil {
			op.B, err = konst(2)
		}
	case "CTX":
		op.Code = OpCtx
		op.A, err = reg(1)
	case "ROOT":
		op.Code = OpRoot
		op.A, err = reg(1)
	case "VAR":
		op.Code = OpVar
		op.A, err = reg(1)
		if err == nil {
			op.B, err = parseID(fields[2], 'S')
		}
	case "MISSING_VAR":
		op.Code = OpMissingVar
		op.A, err = konst(1)
	case "FAIL":
		op.Code = OpFail
		op.A, err = konst(1)
	case "ONE":
		op.Code = OpOne
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
	case "ARG":
		op.Code = OpArg
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
	case "NODE":
		op.Code = OpNode
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
	case "TRUTH":
		op.Code = OpTruth
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
	case "ANY_TRUTH":
		op.Code = OpAnyTruth
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
	case "STEPS":
		op.Code = OpSteps
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
		if err == nil {
			op.C, err = parseID(fields[3], 'P')
		}
	case "OBJECT":
		op.Code = OpObject
		op.A, err = reg(1)
		if err == nil {
			op.B, err = konst(2)
		}
		if err == nil {
			op.C, err = parseID(fields[3], 'A')
		}
	case "ARRAY":
		op.Code = OpArray
		op.A, err = reg(1)
		if err == nil {
			op.B, err = parseID(fields[2], 'A')
		}
	case "UNARY":
		op.Code = OpUnary
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
		if err == nil {
			op.C, err = konst(3)
		}
	case "BINARY":
		op.Code = OpBinary
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
		if err == nil {
			op.C, err = reg(3)
		}
		if err == nil {
			op.D, err = konst(4)
		}
	case "JUMP":
		op.Code = OpJump
		op.A, err = number(fields, 1)
	case "JUMP_IF_FALSE", "JUMP_IF_TRUE", "JUMP_IF_NOT_NULL":
		switch fields[0] {
		case "JUMP_IF_FALSE":
			op.Code = OpJumpIfFalse
		case "JUMP_IF_TRUE":
			op.Code = OpJumpIfTrue
		default:
			op.Code = OpJumpIfNotNull
		}
		op.A, err = reg(1)
		if err == nil {
			op.B, err = number(fields, 2)
		}
	case "PIPE":
		op.Code = OpPipe
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
		if err == nil {
			op.C, err = parseID(fields[3], 'C')
		}
	case "FILTER_STREAM":
		op.Code = OpFilterStream
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
		if err == nil {
			op.C, err = parseID(fields[3], 'C')
		}
	case "FILTER_MATCH":
		op.Code = OpFilterMatch
		op.A, err = reg(1)
		if err == nil {
			op.B, err = reg(2)
		}
		if err == nil {
			op.C, err = parseID(fields[3], 'M')
		}
	case "CALL":
		op.Code = OpCall
		op.A, err = reg(1)
		if err == nil {
			op.B, err = konst(2)
		}
		if err == nil {
			op.C, err = parseID(fields[3], 'A')
		}
	case "SELECT":
		op.Code = OpSelect
		op.A, err = reg(1)
		if err == nil {
			op.B, err = parseID(fields[2], 'C')
		}
	case "TREE":
		op.Code = OpTree
		op.A, err = reg(1)
		if err == nil {
			op.B, err = konst(2)
		}
		if err == nil {
			op.C, err = parseID(fields[3], 'A')
		}
	case "TICK":
		op.Code = OpTick
	case "READ":
		op.Code = OpRead
		op.A, err = reg(1)
	case "READ_VALUE":
		op.Code = OpReadValue
		op.A, err = reg(1)
	case "CHECK_DECLARE":
		op.Code = OpCheckDeclare
		op.A, err = konst(1)
	case "DECLARE":
		op.Code = OpDeclare
		op.A, err = parseID(fields[1], 'S')
		if err == nil {
			op.B, err = reg(2)
		}
	case "ASSIGN":
		op.Code = OpAssign
		op.A, err = konst(1)
		if err == nil {
			op.B, err = parseID(fields[2], 'T')
		}
		if err == nil {
			op.C, err = parseID(fields[3], 'C')
		}
	case "ASSIGN_CONST":
		op.Code = OpAssignConst
		op.A, err = konst(1)
		if err == nil {
			op.B, err = parseID(fields[2], 'T')
		}
		if err == nil {
			op.C, err = konst(3)
		}
	case "DELETE":
		op.Code = OpDelete
		op.A, err = parseID(fields[1], 'D')
	case "FOR_INIT":
		op.Code = OpForInit
		op.A, err = parseID(fields[1], 'L')
		if err == nil {
			op.B, err = parseID(fields[2], 'C')
		}
	case "LOOP_ENTER", "FOR_CLOSE", "LOOP_BREAK", "LOOP_CONTINUE":
		switch fields[0] {
		case "LOOP_ENTER":
			op.Code = OpLoopEnter
		case "FOR_CLOSE":
			op.Code = OpForClose
		case "LOOP_BREAK":
			op.Code = OpLoopBreak
		default:
			op.Code = OpLoopContinue
		}
		op.A, err = parseID(fields[1], 'L')
	case "FOR_NEXT":
		op.Code = OpForNext
		op.A, err = parseID(fields[1], 'L')
		if err == nil {
			op.B, err = number(fields, 2)
		}
	case "ENTER_SCOPE":
		op.Code = OpEnterScope
		op.A, err = parseID(fields[1], 'S')
	case "EXIT_SCOPE":
		op.Code = OpExitScope
	case "STMT_ENTER":
		op.Code = OpStmtEnter
		op.A, err = number(fields, 1)
	case "STMT_LEAVE":
		op.Code = OpStmtLeave
	case "HALT":
		op.Code = OpHalt
	default:
		return Op{}, fmt.Errorf("unknown instruction %q", fields[0])
	}
	if err != nil {
		return Op{}, err
	}
	return op, nil
}

func number(fields []string, index int) (int32, error) {
	if index >= len(fields) {
		return 0, fmt.Errorf("missing number operand")
	}
	value, err := strconv.Atoi(fields[index])
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", fields[index])
	}
	return int32(value), nil
}

func parseID(token string, prefix byte) (int32, error) {
	if len(token) < 2 || token[0] != prefix {
		return 0, fmt.Errorf("expected %cID, got %q", prefix, token)
	}
	value, err := strconv.Atoi(token[1:])
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid ID %q", token)
	}
	return int32(value), nil
}

func parseQuoted(text string) (string, error) {
	var value string
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return "", fmt.Errorf("invalid quoted string: %w", err)
	}
	return value, nil
}

func splitQuoted(line string) (string, string, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, ".param"))
	if !strings.HasPrefix(rest, `"`) {
		return "", "", fmt.Errorf(".param expects a quoted name")
	}
	index := 1
	escaped := false
	for index < len(rest) {
		switch {
		case escaped:
			escaped = false
		case rest[index] == '\\':
			escaped = true
		case rest[index] == '"':
			index++
			name, err := parseQuoted(rest[:index])
			if err != nil {
				return "", "", err
			}
			return name, strings.TrimSpace(rest[index:]), nil
		}
		index++
	}
	return "", "", fmt.Errorf("unterminated parameter name")
}

func parseValue(text string) (any, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "datetime ") {
		value, err := parseQuoted(strings.TrimSpace(strings.TrimPrefix(text, "datetime")))
		if err != nil {
			return nil, err
		}
		return syntax.Temporal(value), nil
	}
	if !strings.HasPrefix(text, "json ") {
		return nil, fmt.Errorf("value must start with json or datetime")
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(strings.TrimPrefix(text, "json"))))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid JSON value: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return normalizeJSON(value)
}

func normalizeJSON(value any) (any, error) {
	switch x := value.(type) {
	case json.Number:
		return syntax.Number(x.String()), nil
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			normalized, err := normalizeJSON(item)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, item := range x {
			normalized, err := normalizeJSON(item)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	default:
		return value, nil
	}
}
