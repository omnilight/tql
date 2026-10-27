// Package bytecode defines the TQL virtual machine instruction set and the
// compiler from the parsed query AST.
package bytecode

import (
	"fmt"

	"github.com/vasileknik76/tql/syntax"
)

// OpCode is a stable bytecode operation number. The values are persisted in
// images and must never be renumbered or reused.
type OpCode uint8

const (
	OpInvalid OpCode = iota
	OpConst
	OpConstValue
	OpCtx
	OpRoot
	OpVar
	OpMissingVar
	OpOne
	OpArg
	OpNode
	OpTruth
	OpSteps
	OpObject
	OpArray
	OpUnary
	OpBinary
	OpJump
	OpJumpIfFalse
	OpJumpIfTrue
	OpJumpIfNotNull
	OpPipe
	OpCall
	OpSelect
	OpTree
	OpTick
	OpRead
	OpCheckDeclare
	OpDeclare
	OpAssign
	OpDelete
	OpForInit
	OpLoopEnter
	OpForNext
	OpForClose
	OpLoopBreak
	OpLoopContinue
	OpEnterScope
	OpExitScope
	OpStmtEnter
	OpStmtLeave
	OpHalt
	OpFilterStream
	OpAssignConst
	OpFail
	OpAnyTruth
	OpReadValue
	OpFilterMatch
)

var opNames = [...]string{
	OpInvalid:       "INVALID",
	OpConst:         "CONST",
	OpConstValue:    "CONST_VALUE",
	OpCtx:           "CTX",
	OpRoot:          "ROOT",
	OpVar:           "VAR",
	OpMissingVar:    "MISSING_VAR",
	OpOne:           "ONE",
	OpArg:           "ARG",
	OpNode:          "NODE",
	OpTruth:         "TRUTH",
	OpSteps:         "STEPS",
	OpObject:        "OBJECT",
	OpArray:         "ARRAY",
	OpUnary:         "UNARY",
	OpBinary:        "BINARY",
	OpJump:          "JUMP",
	OpJumpIfFalse:   "JUMP_IF_FALSE",
	OpJumpIfTrue:    "JUMP_IF_TRUE",
	OpJumpIfNotNull: "JUMP_IF_NOT_NULL",
	OpPipe:          "PIPE",
	OpCall:          "CALL",
	OpSelect:        "SELECT",
	OpTree:          "TREE",
	OpTick:          "TICK",
	OpRead:          "READ",
	OpCheckDeclare:  "CHECK_DECLARE",
	OpDeclare:       "DECLARE",
	OpAssign:        "ASSIGN",
	OpDelete:        "DELETE",
	OpForInit:       "FOR_INIT",
	OpLoopEnter:     "LOOP_ENTER",
	OpForNext:       "FOR_NEXT",
	OpForClose:      "FOR_CLOSE",
	OpLoopBreak:     "LOOP_BREAK",
	OpLoopContinue:  "LOOP_CONTINUE",
	OpEnterScope:    "ENTER_SCOPE",
	OpExitScope:     "EXIT_SCOPE",
	OpStmtEnter:     "STMT_ENTER",
	OpStmtLeave:     "STMT_LEAVE",
	OpHalt:          "HALT",
	OpFilterStream:  "FILTER_STREAM",
	OpAssignConst:   "ASSIGN_CONST",
	OpFail:          "FAIL",
	OpAnyTruth:      "ANY_TRUTH",
	OpReadValue:     "READ_VALUE",
	OpFilterMatch:   "FILTER_MATCH",
}

func (o OpCode) String() string {
	if int(o) < len(opNames) {
		return opNames[o]
	}
	return fmt.Sprintf("OP_%d", o)
}

func (o OpCode) Valid() bool { return o > OpInvalid && int(o) < len(opNames) }

// Op is one instruction. A, B, C and D are register, constant, plan, code,
// loop, scope or jump operands depending on the opcode.
type Op struct {
	Code       OpCode
	A, B, C, D int32
	Stmt       int32
}

// Const is a literal, a text used by an opcode, or an object key set.
type Const struct {
	Value   any
	Text    string
	TextSet bool
	Texts   []string
}

type Param struct {
	Name       string
	Default    any
	HasDefault bool
}

type StepKind uint8

const (
	StepKey StepKind = iota
	StepIndex
	StepWildcard
	StepSlice
	StepFilter
	StepDynamic
	StepMatch
)

type Step struct {
	Kind    StepKind
	Key     string
	Index   int
	From    int
	To      int
	HasFrom bool
	HasTo   bool
	Code    int32
}

type PathPlan struct {
	Steps []Step
}

// MatchPlan is a single key/index lookup followed by optional truthiness or a
// comparison with a literal. It lets the VM evaluate common filters without a
// nested frame.
type MatchPlan struct {
	KeyConst   int32 // text constant, when IsIndex is false
	Index      int32
	IsIndex    bool
	OpConst    int32 // text constant, -1 for a truth test
	ValueConst int32 // value constant, -1 for a truth test
	HasValue   bool
}

type TargetKind uint8

const (
	TargetPath TargetKind = iota
	TargetExpr
)

type TargetPlan struct {
	Kind TargetKind
	Base int32 // code index; -1 means current input
	Path int32 // path plan index for TargetPath; -1 otherwise
	Root bool  // TargetPath starts at the document root
}

type Scope struct {
	Slots []int32
}

type LoopKind uint8

const (
	LoopWhile LoopKind = iota
	LoopFor
)

type Loop struct {
	Kind     LoopKind
	Continue int32
	Break    int32
	Scope    int32
	VarSlot  int32
	ReadOnly bool
}

type Code struct {
	Name      string
	Registers int
	Ops       []Op
}

type Program struct {
	Params     []Param
	Consts     []Const
	Slots      []string
	Paths      []PathPlan
	Targets    []TargetPlan
	TargetSets [][]int32
	ArgSets    [][]int32
	Matches    []MatchPlan
	Scopes     []Scope
	Loops      []Loop
	Codes      []*Code
	Mutates    bool
}

// AddConst appends a constant and returns its index.
func (p *Program) AddConst(c Const) int32 {
	p.Consts = append(p.Consts, c)
	return int32(len(p.Consts) - 1)
}

// AddText appends a text constant and returns its index.
func (p *Program) AddText(s string) int32 { return p.AddConst(Const{Text: s, TextSet: true}) }

// AddValue appends a value constant and returns its index.
func (p *Program) AddValue(v any) int32 { return p.AddConst(Const{Value: v}) }

// AddPath appends a path plan and returns its index.
func (p *Program) AddPath(pth PathPlan) int32 {
	p.Paths = append(p.Paths, pth)
	return int32(len(p.Paths) - 1)
}

// AddTarget appends a target plan and returns its index.
func (p *Program) AddTarget(t TargetPlan) int32 {
	p.Targets = append(p.Targets, t)
	return int32(len(p.Targets) - 1)
}

// AddArgSet appends a register list and returns its index.
func (p *Program) AddArgSet(regs []int32) int32 {
	p.ArgSets = append(p.ArgSets, regs)
	return int32(len(p.ArgSets) - 1)
}

// AddTargetSet appends a code-id list and returns its index.
func (p *Program) AddTargetSet(codes []int32) int32 {
	p.TargetSets = append(p.TargetSets, codes)
	return int32(len(p.TargetSets) - 1)
}

// AddMatch appends a simple filter match plan and returns its index.
func (p *Program) AddMatch(m MatchPlan) int32 {
	p.Matches = append(p.Matches, m)
	return int32(len(p.Matches) - 1)
}

// NormalizeParamDefault keeps parameter defaults in the same value domain as
// execution parameters.
func NormalizeParamDefault(v any) (any, error) { return syntax.NormalizeValue(v) }
