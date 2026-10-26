// Package query parses and evaluates TQL expressions and script syntax.
package query

import (
	"github.com/vasileknik76/tql/syntax"
)

type Expr interface {
	Eval(current, root *syntax.Node, vars ...Variables) ([]*syntax.Node, error)
}

// Variables contains the bindings visible to an expression. Later maps shadow earlier maps.
type Variables map[string]*syntax.Node

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
type Program struct{ Statements []Statement }

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
