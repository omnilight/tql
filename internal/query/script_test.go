package query

import (
	"strings"
	"testing"
)

func TestScriptGrammar(t *testing.T) {
	program, err := Parse(`
 # A script with nested controls and ordinary object literals.
 let $minimum = 2;
 let $stats = {total: 0};
 for $item in walk() {
   if type($item) == "number" {
     if $item < $minimum { continue; }
     $stats.total += $item;
   } else if type($item) == "null" {
     del($item)
   } else { $.untouched; }
   while $stats.total > 10 { $stats.total -= 1; break }
 }
 $.total = $stats.total
 `)
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Statements) != 4 || program.Statements[2].Op != "for" {
		t.Fatalf("unexpected AST: %#v", program)
	}
	loop := program.Statements[2]
	if loop.Name != "item" || len(loop.Body) != 2 || loop.Body[0].Op != "if" || loop.Body[1].Op != "while" {
		t.Fatalf("unexpected loop: %#v", loop)
	}
	branch := loop.Body[0]
	if len(branch.Else) != 1 || branch.Else[0].Op != "if" || len(branch.Else[0].Else) != 1 {
		t.Fatalf("unexpected else chain: %#v", branch)
	}
	if !program.Mutates() {
		t.Fatal("nested assignments must mark program mutating")
	}
	for _, source := range []string{
		`let $x = 1; if $x { $x } else { null }`,
		`for $n in walk() { if false { break } else { continue } }`,
		`while false { if true {} }; let $x = 1`,
	} {
		p, err := Parse(source)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		if p.Mutates() {
			t.Fatalf("read-only script marked mutating: %s", source)
		}
	}
}

func TestScriptNestingLimit(t *testing.T) {
	source := strings.Repeat("if true {", 128) + "null" + strings.Repeat("}", 128)
	if _, err := Parse(source); err != nil {
		t.Fatalf("128 blocks must be accepted: %v", err)
	}
	source = strings.Repeat("if true {", 129) + "null" + strings.Repeat("}", 129)
	if _, err := Parse(source); err == nil {
		t.Fatal("129 blocks accepted")
	}
}

func TestScriptInvalidGrammar(t *testing.T) {
	for _, source := range []string{
		`let x = 1`, `let $ = 1`, `let $ x = 1`, `let $x`, `let $x =`,
		`let $x = 1 let $y = 2`, `if true`, `if true {`, `if true { x = 1 y = 2 }`,
		`if true {} else`, `if true {} else while true {}`, `if true {} else if {}`,
		`for x in walk() {}`, `for $x walk() {}`, `for $x in {}`, `while {}`,
		`break`, `continue`, `if true { break }`, `for $x in [] {} continue`,
		`if true {} }`, strings.Repeat(`if true {`, 129) + strings.Repeat(`}`, 129),
		strings.Repeat(`if false {} else `, 129) + `if true {}`,
	} {
		if _, err := Parse(source); err == nil {
			t.Errorf("accepted invalid script: %s", source)
		}
	}
}
