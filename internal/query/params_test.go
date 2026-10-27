package query

import (
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestParseParameters(t *testing.T) {
	p, err := Parse(`# inputs
param $required;
param $optional = {a: [-1, +2.5, true, false, null, "x"]};
$required`)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Parameters) != 2 || p.Parameters[0].Name != "required" || p.Parameters[0].HasDefault || !p.Parameters[1].HasDefault || p.Mutates() || len(p.Statements) != 1 {
		t.Fatalf("program=%+v", p)
	}
	value, err := syntax.JSON(p.Parameters[1].Default)
	if err != nil || value != `{"a":[-1,2.5,true,false,null,"x"]}` {
		t.Fatalf("default=%s err=%v", value, err)
	}
	for _, script := range []string{`param`, `param.field`, `param = 1`, `param["name"]`, `param $x`, `param $x = null;`} {
		if _, err := Parse(script); err != nil {
			t.Errorf("%s: %v", script, err)
		}
	}
}

func TestInvalidParameterDeclarations(t *testing.T) {
	for _, script := range []string{
		`param $x; param $x;`, `param $x param $y`, `param $x =`, `param $ x`, `param $.x`,
		`param $true`, `param $x = field`, `param $x = $y`, `param $x = length([])`,
		`param $x = [1 + 2]`, `param $x = {v: .}`, `param $x = -true`,
		`1; param $x`, `if true { param $x; }`, `for $n in [] { param $x }`,
		`while false { param $x = 1; }`, `param $x = [1][]`,
	} {
		if _, err := Parse(script); err == nil {
			t.Errorf("accepted %s", script)
		}
	}
	if _, err := Parse(`param $x = ` + strings.Repeat("[", 300) + `1` + strings.Repeat("]", 300)); err == nil {
		t.Fatal("accepted excessive default nesting")
	}
}

func TestParameterNames(t *testing.T) {
	for _, name := range []string{"x", "_", "name_2", "имя", "param", "let"} {
		if !ValidVariableName(name) {
			t.Errorf("rejected name %q", name)
		}
	}
	for _, name := range []string{"", "$x", "1x", "true", "and", "a.b", "a-b", " a", "a ", "a;", string([]byte{0xff})} {
		if ValidVariableName(name) {
			t.Errorf("accepted name %q", name)
		}
	}
}
