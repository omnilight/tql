package tql

import (
	"fmt"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func runBoth(t *testing.T, source, program string, options ...ExecOption) (*Result, *Result, error, error) {
	t.Helper()
	makeDoc := func() *Document {
		d, err := Parse([]byte(source), "json")
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	astDoc, vmDoc := makeDoc(), makeDoc()
	p, err := Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	o, err := executionOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	ast, err1 := astDoc.executeAST(p, true, o)
	vm, err2 := vmDoc.executeVM(p, true, o)
	return ast, vm, err1, err2
}

func values(t *testing.T, result *Result) []string {
	t.Helper()
	out := make([]string, len(result.Values))
	for i, value := range result.Values {
		s, err := syntax.JSON(value)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = s
	}
	return out
}

func TestVMExpressionParity(t *testing.T) {
	source := `{"a":[1,2,3],"b":{"x":1,"y":2},"items":[{"name":"api","n":1},{"name":"web","n":2}],"s":"café"}`
	programs := []string{
		`a`, `a[0]`, `a[-1]`, `a[*]`, `a[0:2]`, `a[] | select(. > 1)`,
		`b.x`, `b.*`, `items[name == "api"].n`, `items[].n`,
		`1 + 2 * 3`, `"a" + "b"`, `not false`, `null ?? 3`, `false ?? 3`,
		`{x: 1, y: a}`, `[1, 2, a[]]`, `length(a)`, `keys(b)`, `values(b)`,
		`type(a)`, `sort(a)`, `reverse(a)`, `first(a)`, `last(a)`,
		`has(b, "x")`, `contains(a, 2)`, `startswith(s, "ca")`,
		`endswith(s, "fé")`, `test(s, "^ca")`, `select(true)`,
		`walk(a) | select(type == "number")`, `path(a[1])`, `key(a[1])`,
		`a | reverse`, `s`, `s[0]`,
	}
	for _, program := range programs {
		t.Run(program, func(t *testing.T) {
			ast, vm, e1, e2 := runBoth(t, source, program)
			if (e1 == nil) != (e2 == nil) {
				t.Fatalf("error mismatch: AST=%v VM=%v", e1, e2)
			}
			if e1 != nil {
				if e1.Error() != e2.Error() {
					t.Fatalf("error text: AST=%q VM=%q", e1, e2)
				}
				return
			}
			av, bv := values(t, ast), values(t, vm)
			if fmt.Sprint(av) != fmt.Sprint(bv) {
				t.Fatalf("values: AST=%v VM=%v", av, bv)
			}
			if string(ast.Bytes) != string(vm.Bytes) {
				t.Fatalf("bytes: AST=%s VM=%s", ast.Bytes, vm.Bytes)
			}
			if ast.Changed != vm.Changed {
				t.Fatalf("changed: AST=%v VM=%v", ast.Changed, vm.Changed)
			}
		})
	}
}

func TestVMStatementParity(t *testing.T) {
	source := `{"n":0,"items":[{"n":1},{"n":2}],"a":[1,2,3],"s":"x"}`
	programs := []string{
		`let $count = 0; n = 1; $count`,
		`let $x = 1; if true { let $x = 10; $x += 1; $x } $x`,
		`for $item in items[] { $item.n += 1; } items`,
		`let $i = 0; while $i < 3 { a[$i] += 1; $i += 1 } a`,
		`for $x in a[] { if $x == 2 { continue } $x += 10 } a`,
		`for $x in a[] { if $x == 2 { break } $x += 10 } a`,
		`new.deep[0].key = "é"; new`,
		`items[].missing ??= 2; items`,
		`a[1] += 4; a`,
		`del(a[0], a[-1]); a`,
		`let $copy = items; for $x in $copy[] { $x.n += 1 } $copy`,
		`for $x in a[] { if $x == 1 { del($.a[1]) } $x += 10 } a`,
		`let $keys = []; for $x in a[] { $keys += [key($x)] } $keys`,
		`param $delta = 2; a[] += $delta; a`,
		`let $o = {x: 1}; $o.x += 2; $o`,
		`$a = 4; a`,
		`for $x in a[] { key($x) }`,
		`for $x in a[] { path($x) }`,
	}
	for _, program := range programs {
		t.Run(program, func(t *testing.T) {
			ast, vm, e1, e2 := runBoth(t, source, program)
			if (e1 == nil) != (e2 == nil) {
				t.Fatalf("error mismatch: AST=%v VM=%v", e1, e2)
			}
			if e1 != nil {
				if e1.Error() != e2.Error() {
					t.Fatalf("error text: AST=%q VM=%q", e1, e2)
				}
				return
			}
			if fmt.Sprint(values(t, ast)) != fmt.Sprint(values(t, vm)) {
				t.Fatalf("values: AST=%v VM=%v", values(t, ast), values(t, vm))
			}
			if string(ast.Bytes) != string(vm.Bytes) {
				t.Fatalf("bytes: AST=%s VM=%s", ast.Bytes, vm.Bytes)
			}
		})
	}
}

func TestVMErrorParity(t *testing.T) {
	source := `{"n":0,"items":[1,2,3]}`
	programs := []string{
		`n = 1; let $x = 2; let $x = 3`,
		`n = 1; if true { let $hidden = 1 } n = $hidden`,
		`n = 1 / 0`,
		`for $n in items[] { del($n); $n += 1 }`,
		`n = 1; while true {}`,
		`$unknown`,
	}
	for _, program := range programs {
		t.Run(program, func(t *testing.T) {
			_, _, e1, e2 := runBoth(t, source, program)
			if e1 == nil || e2 == nil {
				t.Fatalf("expected both to fail: AST=%v VM=%v", e1, e2)
			}
			if e1.Error() != e2.Error() {
				t.Fatalf("error text: AST=%q VM=%q", e1, e2)
			}
		})
	}
}

func FuzzExecParity(f *testing.F) {
	for _, query := range []string{
		`a`, `a[]`, `a[] += 1; a`, `del(a[0]); a`,
		`let $x = 0; for $x in a[] { $x += 1 } a`,
		`a[name == "x"].v = 1; a`, `a[-1] ??= 2; a`,
		`for $x in walk(.) { if type($x) == "number" { $x += 1 } }`,
	} {
		f.Add(query)
	}
	f.Fuzz(func(t *testing.T, query string) {
		if len(query) > 4096 {
			t.Skip()
		}
		p, err := Compile(query)
		if err != nil {
			return
		}
		source := []byte(`{"a":[1,2,3],"b":null}`)
		astDoc, err := Parse(source, "json")
		if err != nil {
			t.Fatal(err)
		}
		vmDoc, err := Parse(source, "json")
		if err != nil {
			t.Fatal(err)
		}
		ast, e1 := astDoc.executeAST(p, true, execOptions{})
		vm, e2 := vmDoc.executeVM(p, true, execOptions{})
		if (e1 == nil) != (e2 == nil) {
			t.Fatalf("error mismatch: AST=%v VM=%v", e1, e2)
		}
		if e1 != nil {
			if e1.Error() != e2.Error() {
				t.Fatalf("error text: AST=%q VM=%q", e1, e2)
			}
			return
		}
		if string(ast.Bytes) != string(vm.Bytes) {
			t.Fatalf("bytes: AST=%s VM=%s", ast.Bytes, vm.Bytes)
		}
		if fmt.Sprint(values(t, ast)) != fmt.Sprint(values(t, vm)) {
			t.Fatalf("values: AST=%v VM=%v", values(t, ast), values(t, vm))
		}
	})
}
