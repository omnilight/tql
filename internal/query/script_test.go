package query

import (
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
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

func TestVariablesInEveryExpression(t *testing.T) {
	root := Node(map[string]any{"items": []any{syntax.Number("1"), syntax.Number("3")}, "name": "example"})
	vars := Variables{"x": Node(syntax.Number("2")), "name": Node("example"), "index": Node(syntax.Number("1")), "key": Node("items")}
	tests := []struct{ source, want string }{
		{`-$x + 5`, `3`}, {`[$x, $x * 2]`, `[2,4]`},
		{`{n:$x, test: $name}`, `{"n":2,"test":"example"}`},
		{`items[. > $x]`, `3`}, {`items[] | select(. > $x)`, `3`},
		{`items[$index]`, `3`}, {`.[$key][$index]`, `3`}, {`$.name == $name`, `true`},
		{`length($name)`, `7`}, {`startswith($name, "ex")`, `true`},
		{`$name | endswith("ple")`, `true`}, {`type($x)`, `"number"`},
	}
	for _, tt := range tests {
		p, err := Parse(tt.source)
		if err != nil {
			t.Fatalf("%s: %v", tt.source, err)
		}
		value, err := One(p.Statements[0].Target, root, root, vars)
		if err != nil {
			t.Fatalf("%s: %v", tt.source, err)
		}
		got, _ := syntax.JSON(value)
		if got != tt.want {
			t.Errorf("%s = %s; want %s", tt.source, got, tt.want)
		}
	}
	v, err := One(Variable{Name: "x"}, root, root, vars, Variables{"x": Node("shadow")})
	if err != nil || v != "shadow" {
		t.Fatalf("scope shadow: %v %v", v, err)
	}
	if _, err := (Variable{Name: "undefined"}).Eval(root, root, vars); err == nil {
		t.Fatal("undefined variable accepted")
	}
	if _, err := (Variable{Name: "x"}).Eval(root, root, Variables{"x": nil}); err == nil {
		t.Fatal("empty binding accepted")
	}
}

func TestDynamicSelector(t *testing.T) {
	root := Node([]any{syntax.Number("3"), syntax.Number("4")})
	p, err := Parse(`.[$i]`)
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{"1", "1.0", "1e0", "-1"} {
		v, err := One(p.Statements[0].Target, root, root, Variables{"i": Node(syntax.Number(number))})
		if err != nil || v != syntax.Number("4") {
			t.Errorf("index %s = %v %v", number, v, err)
		}
	}
	for _, value := range []any{true, nil, syntax.Number("1.5"), syntax.Number("1e10001"), syntax.Number("99999999999999999999")} {
		if _, err := One(p.Statements[0].Target, root, root, Variables{"i": Node(value)}); err == nil {
			t.Errorf("invalid selector %v accepted", value)
		}
	}
	for _, source := range []string{`$x = 1`, `$x.a = 2`, `del($x.a)`, `obj[$x] = 3`, `walk() | select(type == "number") |= . + 1`, `walk($.obj) |= .`} {
		if _, err := Parse(source); err != nil {
			t.Fatalf("selector: %s: %v", source, err)
		}
	}
}

func TestTreeFunctions(t *testing.T) {
	// Explicit object order checks walk follows the source tree rather than sorting keys.
	root := &syntax.Node{Kind: syntax.Object, Entries: []*syntax.Entry{
		{Key: "z", Value: Node([]any{syntax.Number("1"), syntax.Number("2")})},
		{Key: "a", Value: Node(true)},
	}}
	syntax.Link(root)
	tests := []struct{ source, want string }{
		{`[walk() | path()]`, `[["z",0],["z",1],["z"],["a"],[]]`},
		{`[walk() | key()]`, `[0,1,"z","a",null]`},
		{`[path(z[])]`, `[["z",0],["z",1]]`},
		{`[key(z[])]`, `[0,1]`},
		{`[walk(z) | path()]`, `[["z",0],["z",1],["z"]]`},
	}
	for _, tt := range tests {
		p, err := Parse(tt.source)
		if err != nil {
			t.Fatal(err)
		}
		value, err := One(p.Statements[0].Target, root, root)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := syntax.JSON(value)
		if got != tt.want {
			t.Errorf("%s = %s; want %s", tt.source, got, tt.want)
		}
	}
	ns, err := (Call{Name: "walk"}).Eval(root, root)
	if err != nil || len(ns) != 5 || ns[0] != root.Get("z").Items[0] || ns[4] != root {
		t.Fatalf("walk lost live pointers: %v %v", ns, err)
	}
	for _, source := range []string{`walk(1,2)`, `path(1)`, `key([])`, `path(1,2)`, `key(1,2)`} {
		p, err := Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Statements[0].Target.Eval(root, root); err == nil {
			t.Errorf("accepted invalid tree call %s", source)
		}
	}
}

func BenchmarkWalkType(b *testing.B) {
	root := Node(nil)
	// A chain makes accidental subtree materialization quadratic.
	for i := 0; i < 1000; i++ {
		root = &syntax.Node{Kind: syntax.Array, Items: []*syntax.Node{root}}
	}
	syntax.Link(root)
	p, err := Parse(`walk() | type()`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Statements[0].Target.Eval(root, root); err != nil {
			b.Fatal(err)
		}
	}
}

func TestVariableDerivedDynamicSelectors(t *testing.T) {
	root := Node(map[string]any{
		"items": []any{syntax.Number("10"), syntax.Number("20"), syntax.Number("30")},
		"obj":   map[string]any{"selected": "yes", "fallback": "selected"},
	})
	vars := Variables{
		"i":     Node(syntax.Number("2")),
		"entry": Node(map[string]any{"index": syntax.Number("1"), "key": "selected"}),
		"unset": Node(nil),
	}
	for _, tc := range []struct{ source, want string }{
		{`items[$i - 1]`, `20`},
		{`items[$entry.index]`, `20`},
		{`items[+$i]`, `30`},
		{`items[-$i]`, `20`},
		{`items[$i * 1]`, `30`},
		{`items[4 / $i]`, `30`},
		{`items[$i % 2]`, `10`},
		{`items[$unset ?? 1]`, `20`},
		{`obj[$entry.key]`, `"yes"`},
		// The non-variable operand sees the indexed parent, never each item.
		{`items[$i - length(.) + 2]`, `20`},
		{`obj[$unset ?? .fallback]`, `"yes"`},
	} {
		p, err := Parse(tc.source)
		if err != nil {
			t.Fatal(err)
		}
		path, ok := p.Statements[0].Target.(Path)
		if !ok || path.Steps[len(path.Steps)-1].Kind != "dynamic" {
			t.Fatalf("%s must be dynamic", tc.source)
		}
		value, err := One(path, root, root, vars)
		if err != nil {
			t.Fatalf("%s: %v", tc.source, err)
		}
		got, _ := syntax.JSON(value)
		if got != tc.want {
			t.Errorf("%s = %s, want %s", tc.source, got, tc.want)
		}
	}
	for _, source := range []string{
		`items[$i / 4]`, `items[$i + "bad"]`, `items[$entry]`,
		`items[$entry.missing]`, `items[$unset ?? false]`,
	} {
		p, err := Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Statements[0].Target.Eval(root, root, vars); err == nil {
			t.Errorf("invalid dynamic index accepted: %s", source)
		}
	}
	for _, source := range []string{
		`items[. == $i]`, `items[. > $i]`, `items[. != $i and . < 30]`,
		`items[$i == 2 or . == 10]`,
	} {
		p, err := Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		path := p.Statements[0].Target.(Path)
		if path.Steps[len(path.Steps)-1].Kind != "filter" {
			t.Fatalf("comparison/logical expression must remain a filter: %s", source)
		}
	}
}
