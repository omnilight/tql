package query

import (
	"fmt"
	"strconv"

	"github.com/vasileknik76/tql/syntax"
)

type parser struct {
	tokens             []token
	at, depth          int
	scriptDepth, loops int
	parameters         []Parameter
	parameterNames     map[string]bool
	started            bool
}

func Parse(s string) (*Program, error) {
	ts, err := lex(s)
	if err != nil {
		return nil, err
	}
	p := parser{tokens: ts}
	statements, err := p.statements(false)
	if err != nil {
		return nil, err
	}
	if len(statements) == 0 && len(p.parameters) == 0 {
		return nil, fmt.Errorf("TQL: empty query")
	}
	return &Program{Parameters: p.parameters, Statements: statements}, nil
}

func (p *parser) statements(block bool) ([]Statement, error) {
	var out []Statement
	for p.peek().kind != "eof" && (!block || p.peek().kind != "}") {
		if p.take(";") {
			continue
		}
		if p.peek().kind == "ident" && p.peek().text == "param" && p.tokens[p.at+1].kind == "$" {
			if block || p.started {
				return nil, p.err("parameters must be declared before other statements")
			}
			if err := p.parameter(); err != nil {
				return nil, err
			}
			if !p.take(";") && p.peek().kind != "eof" {
				return nil, p.err("expected ';' between declarations")
			}
			continue
		}
		p.started = true
		st, compound, err := p.statement()
		if err != nil {
			return nil, err
		}
		out = append(out, st)
		if !p.take(";") && !compound && p.peek().kind != "eof" && (!block || p.peek().kind != "}") {
			return nil, p.err("expected ';' between statements")
		}
	}
	if block {
		if err := p.expect("}"); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (p *parser) parameter() error {
	p.next()
	name, err := p.variableName()
	if err != nil {
		return err
	}
	if p.parameterNames[name] {
		return p.err("duplicate parameter $" + name)
	}
	param := Parameter{Name: name}
	if p.take("=") {
		e, err := p.expr(0)
		if err != nil {
			return err
		}
		if !constant(e) {
			return p.err("parameter default must be a constant value")
		}
		param.Default, err = constantValue(e)
		if err != nil {
			return err
		}
		param.HasDefault = true
	}
	p.parameters = append(p.parameters, param)
	if p.parameterNames == nil {
		p.parameterNames = make(map[string]bool)
	}
	p.parameterNames[name] = true
	return nil
}

func constant(e Expr) bool {
	switch x := e.(type) {
	case Literal:
		return true
	case Unary:
		literal, ok := x.Expr.(Literal)
		if !ok {
			return false
		}
		_, number := literal.Value.(syntax.Number)
		return number && (x.Op == "+" || x.Op == "-")
	case Array:
		for _, value := range x.Values {
			if !constant(value) {
				return false
			}
		}
		return true
	case Object:
		for _, value := range x.Values {
			if !constant(value) {
				return false
			}
		}
		return true
	}
	return false
}

// ValidVariableName accepts exactly one named variable, without its leading $.
func ValidVariableName(name string) bool {
	ts, err := lex("$" + name)
	return err == nil && len(ts) == 3 && ts[1].kind == "ident" && ts[1].pos == 1 && ts[1].text == name
}
func (p *parser) block() ([]Statement, error) {
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	return p.statements(true)
}
func (p *parser) variableName() (string, error) {
	t := p.next()
	if t.kind != "$" || p.peek().kind != "ident" || p.peek().pos != t.pos+1 {
		return "", p.err("expected named variable such as $item")
	}
	return p.next().text, nil
}
func (p *parser) statement() (Statement, bool, error) {
	p.scriptDepth++
	defer func() { p.scriptDepth-- }()
	st := Statement{Op: "read"}
	if p.peek().kind == "ident" {
		switch p.peek().text {
		case "let":
			p.next()
			st.Op = "let"
			var err error
			st.Name, err = p.variableName()
			if err != nil {
				return st, false, err
			}
			if err = p.expect("="); err != nil {
				return st, false, err
			}
			st.Value, err = p.expr(0)
			return st, false, err
		case "if", "while", "for":
			if p.scriptDepth > 128 {
				return st, true, p.err("script nesting limit exceeded")
			}
			st.Op = p.next().text
			var err error
			if st.Op == "for" {
				st.Name, err = p.variableName()
				if err != nil {
					return st, true, err
				}
				if p.peek().kind != "ident" || p.peek().text != "in" {
					return st, true, p.err("expected 'in'")
				}
				p.next()
			}
			st.Value, err = p.expr(0)
			if err != nil {
				return st, true, err
			}
			if st.Op != "if" {
				p.loops++
			}
			st.Body, err = p.block()
			if st.Op != "if" {
				p.loops--
			}
			if err != nil {
				return st, true, err
			}
			if st.Op == "if" && p.peek().kind == "ident" && p.peek().text == "else" {
				p.next()
				if p.peek().kind == "ident" && p.peek().text == "if" {
					child, _, e := p.statement()
					err = e
					st.Else = []Statement{child}
				} else {
					st.Else, err = p.block()
				}
			}
			return st, true, err
		case "break", "continue":
			st.Op = p.next().text
			if p.loops == 0 {
				return st, false, p.err(st.Op + " requires a loop")
			}
			return st, false, nil
		}
	}
	if p.peek().text == "del" && p.at+1 < len(p.tokens) && p.tokens[p.at+1].kind == "(" {
		p.at += 2
		st.Op = "del"
		for {
			e, err := p.expr(0)
			if err != nil {
				return st, false, err
			}
			if !selector(e) {
				return st, false, p.err("del requires document paths")
			}
			st.Targets = append(st.Targets, e)
			if !p.take(",") {
				break
			}
		}
		return st, false, p.expect(")")
	}
	e, err := p.expr(0)
	if err != nil {
		return st, false, err
	}
	st.Target = e
	switch p.peek().kind {
	case "=", "|=", "+=", "-=", "*=", "/=", "??=":
		st.Op = p.next().kind
		if !selector(st.Target) {
			return st, false, p.err("assignment requires a document path or variable")
		}
		st.Value, err = p.expr(0)
	}
	return st, false, err
}
func (p *parser) peek() token { return p.tokens[p.at] }
func (p *parser) next() token {
	t := p.peek()
	if t.kind != "eof" {
		p.at++
	}
	return t
}
func (p *parser) take(k string) bool {
	if p.peek().kind == k {
		p.at++
		return true
	}
	return false
}
func (p *parser) expect(k string) error {
	if !p.take(k) {
		return p.err("expected " + strconv.Quote(k))
	}
	return nil
}
func (p *parser) err(s string) error {
	return fmt.Errorf("TQL at byte %d: %s (found %q)", p.peek().pos+1, s, p.peek().text)
}

var precedence = map[string]int{"|": 1, "??": 2, "or": 3, "and": 4, "==": 5, "!=": 5, "<": 6, "<=": 6, ">": 6, ">=": 6, "+": 7, "-": 7, "*": 8, "/": 8, "%": 8}

func (p *parser) expr(minPrec int) (Expr, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 256 {
		return nil, p.err("expression nesting limit exceeded")
	}
	left, err := p.prefix()
	if err != nil {
		return nil, err
	}
	operators := 0
	for {
		op := p.peek().kind
		prec := precedence[op]
		if prec == 0 || prec < minPrec {
			break
		}
		operators++
		if operators > 256 {
			return nil, p.err("expression exceeds 256 operators")
		}
		p.at++
		right, err := p.expr(prec + 1)
		if err != nil {
			return nil, err
		}
		left = Binary{op, left, right}
	}
	return left, nil
}
func (p *parser) prefix() (Expr, error) {
	t := p.next()
	var e Expr
	switch t.kind {
	case "number":
		e = Literal{syntax.Number(t.text)}
	case "string":
		e = Literal{t.text}
	case "true":
		e = Literal{true}
	case "false":
		e = Literal{false}
	case "null":
		e = Literal{nil}
	case "-", "+", "not", "!":
		v, err := p.expr(9)
		if err != nil {
			return nil, err
		}
		return Unary{t.kind, v}, nil
	case "(":
		v, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		if err = p.expect(")"); err != nil {
			return nil, err
		}
		e = v
	case "$":
		if p.peek().kind == "ident" && p.peek().pos == t.pos+1 {
			e = Variable{Name: p.next().text}
		} else {
			e = Path{Root: true}
		}
	case ".":
		path := Path{}
		if p.peek().kind == "ident" {
			path.Steps = append(path.Steps, Step{Kind: "key", Key: p.next().text})
		} else if p.peek().kind == "*" && p.peek().pos == t.pos+1 {
			p.at++
			path.Steps = append(path.Steps, Step{Kind: "wildcard"})
		}
		e = path
	case "ident":
		if p.take("(") {
			call := Call{Name: t.text}
			if !p.take(")") {
				for {
					v, err := p.expr(0)
					if err != nil {
						return nil, err
					}
					call.Args = append(call.Args, v)
					if !p.take(",") {
						break
					}
				}
				if err := p.expect(")"); err != nil {
					return nil, err
				}
			}
			e = call
		} else if isBareFunction(t.text) {
			e = Call{Name: t.text}
		} else {
			e = Path{Steps: []Step{{Kind: "key", Key: t.text}}}
		}
	case "[":
		a := Array{}
		if !p.take("]") {
			for {
				v, err := p.expr(0)
				if err != nil {
					return nil, err
				}
				a.Values = append(a.Values, v)
				if !p.take(",") {
					break
				}
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
		}
		e = a
	case "{":
		o := Object{}
		seen := map[string]bool{}
		if !p.take("}") {
			for {
				k := p.next()
				if k.kind != "ident" && k.kind != "string" {
					return nil, p.err("expected object key")
				}
				if seen[k.text] {
					return nil, p.err("duplicate object key")
				}
				seen[k.text] = true
				if err := p.expect(":"); err != nil {
					return nil, err
				}
				v, err := p.expr(0)
				if err != nil {
					return nil, err
				}
				o.Keys = append(o.Keys, k.text)
				o.Values = append(o.Values, v)
				if !p.take(",") {
					break
				}
			}
			if err := p.expect("}"); err != nil {
				return nil, err
			}
		}
		e = o
	default:
		return nil, p.err("expected expression")
	}
	for {
		var step Step
		if p.take(".") {
			if p.take("*") {
				step.Kind = "wildcard"
			} else {
				k := p.next()
				if k.kind != "ident" {
					return nil, p.err("expected field name")
				}
				step = Step{Kind: "key", Key: k.text}
			}
		} else if p.take("[") {
			if p.take("]") {
				step.Kind = "wildcard"
			} else if p.take("*") {
				step.Kind = "wildcard"
				if err := p.expect("]"); err != nil {
					return nil, err
				}
			} else {
				// Literal strings/integers index; a colon makes a half-open slice.
				save := p.at
				var idx *int
				sign := ""
				if p.take("-") {
					sign = "-"
				}
				if p.peek().kind == "number" {
					text := sign + p.next().text
					if p.peek().kind == ":" || p.peek().kind == "]" {
						v, er := strconv.Atoi(text)
						if er != nil {
							return nil, p.err("index or slice bound must be an in-range integer")
						}
						idx = &v
					}
				}
				if p.take(":") {
					if sign != "" && idx == nil {
						return nil, p.err("expected integer after '-' in slice bound")
					}
					step.Kind = "slice"
					step.From = idx
					if p.peek().kind != "]" {
						sign = ""
						if p.take("-") {
							sign = "-"
						}
						if p.peek().kind != "number" {
							return nil, p.err("slice bound must be an integer")
						}
						v, er := strconv.Atoi(sign + p.next().text)
						if er != nil {
							return nil, p.err("slice bound must be an in-range integer")
						}
						step.To = &v
					}
					if err := p.expect("]"); err != nil {
						return nil, err
					}
				} else if idx != nil && p.take("]") {
					step = Step{Kind: "index", Index: *idx}
				} else {
					p.at = save
					if p.peek().kind == "string" && p.at+1 < len(p.tokens) && p.tokens[p.at+1].kind == "]" {
						step = Step{Kind: "key", Key: p.next().text}
						p.at++
					} else {
						v, err := p.expr(0)
						if err != nil {
							return nil, err
						}
						step = Step{Kind: "filter", Predicate: v}
						if dynamicSelector(v) {
							step.Kind = "dynamic"
						}
						if err := p.expect("]"); err != nil {
							return nil, err
						}
					}
				}
			}
		} else {
			break
		}
		if path, ok := e.(Path); ok {
			if len(path.Steps) >= 256 {
				return nil, p.err("path exceeds 256 steps")
			}
			path.Steps = append(path.Steps, step)
			e = path
		} else {
			e = Path{Base: e, Steps: []Step{step}}
		}
	}
	return e, nil
}
func isBareFunction(s string) bool {
	switch s {
	case "length", "keys", "values", "type", "sort", "reverse", "first", "last":
		return true
	}
	return false
}
func dynamicSelector(e Expr) bool {
	switch x := e.(type) {
	case Variable:
		return true
	case Path:
		return x.Base != nil && dynamicSelector(x.Base)
	case Unary:
		return (x.Op == "+" || x.Op == "-") && dynamicSelector(x.Expr)
	case Binary:
		switch x.Op {
		case "+", "-", "*", "/", "%", "??":
			return dynamicSelector(x.Left) || dynamicSelector(x.Right)
		}
	}
	return false
}

func selector(e Expr) bool {
	switch x := e.(type) {
	case Variable:
		return true
	case Path:
		return x.Base == nil || selector(x.Base)
	case Binary:
		return x.Op == "|" && selector(x.Left) && selector(x.Right)
	case Call:
		return (x.Name == "select" && len(x.Args) == 1) || (x.Name == "walk" && (len(x.Args) == 0 || (len(x.Args) == 1 && selector(x.Args[0]))))
	}
	return false
}
