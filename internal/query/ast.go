// Package query parses TQL expressions and script syntax for bytecode compilation.
package query

// Expr is an AST expression node. Concrete node types are inspected by the
// bytecode compiler; parsing does not evaluate them.
type Expr interface{}

type Variable struct{ Name string }
type Literal struct{ Value any }
type Path struct {
	Base  Expr
	Root  bool
	Steps []Step
}
type Step struct {
	Kind      string
	Key       string
	Index     int
	From, To  *int
	Predicate Expr
}
type Binary struct {
	Op          string
	Left, Right Expr
}
type Unary struct {
	Op   string
	Expr Expr
}
type Call struct {
	Name string
	Args []Expr
}
type Object struct {
	Keys   []string
	Values []Expr
}
type Array struct{ Values []Expr }
type Statement struct {
	Name       string
	Body, Else []Statement
	Op         string
	Target     Expr
	Value      Expr
	Targets    []Expr
}
type Parameter struct {
	Name       string
	Default    any
	HasDefault bool
}
type Program struct {
	Parameters []Parameter
	Statements []Statement
}

func (p *Program) Mutates() bool { return mutates(p.Statements) }
func mutates(statements []Statement) bool {
	for _, s := range statements {
		switch s.Op {
		case "if", "for", "while":
			if mutates(s.Body) || mutates(s.Else) {
				return true
			}
		case "read", "let", "break", "continue":
		default:
			return true
		}
	}
	return false
}
