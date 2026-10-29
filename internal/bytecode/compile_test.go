package bytecode

import (
	"testing"

	"github.com/vasileknik76/tql/internal/query"
)

func TestCompileProgramShapes(t *testing.T) {
	source := `
param $delta = 2;
let $count = 0;
for $item in items[name == "api"] {
    if $item.enabled {
        $item.port += $delta;
        $count += 1;
    } else {
        del($item.old);
    }
}
while $count < 10 { $count += 1; }
$count
`
	ast, err := query.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(ast)
	if err != nil {
		t.Fatal(err)
	}
	if len(prog.Codes) == 0 || prog.Codes[0].Name != "main" {
		t.Fatalf("missing main code: %#v", prog.Codes)
	}
	if !prog.Mutates {
		t.Fatal("program should mutate")
	}
	if len(prog.Params) != 1 || prog.Params[0].Name != "delta" {
		t.Fatalf("params=%#v", prog.Params)
	}
	if len(prog.Loops) != 2 {
		t.Fatalf("loops=%d", len(prog.Loops))
	}
	if len(prog.Paths) == 0 || len(prog.Targets) == 0 {
		t.Fatalf("paths=%d targets=%d", len(prog.Paths), len(prog.Targets))
	}
	for i, code := range prog.Codes {
		if len(code.Ops) == 0 || code.Ops[len(code.Ops)-1].Code != OpHalt {
			t.Fatalf("code %d does not end in HALT: %#v", i, code.Ops)
		}
	}
}

func TestCompileEveryExpression(t *testing.T) {
	source := `
let $key = "port";
let $index = 1;
[a, b[0], c[*], d[key == "x"], e[$key], f[1:2], g[*], h[$index - 1],
 {x: 1 + 2, y: -3, z: not false, w: a ?? b, v: a and b, u: a or b},
 length(a), keys(a), values(a), type(a), sort(a), reverse(a), first(a), last(a),
 has(a, "x"), contains(a, "x"), startswith("a", "a"), endswith("a", "a"),
 test("a", "a"), select(a), walk(a), path(a), key(a), a | b]
`
	ast, err := query.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(ast)
	if err != nil {
		t.Fatal(err)
	}
	if len(prog.Codes) < 2 {
		t.Fatalf("expected sub-codes, got %d", len(prog.Codes))
	}
}
