package tql_test

import (
	"bytes"
	"sync"
	"testing"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/syntax"
)

func TestScriptScopesSnapshotsAndLocalMutation(t *testing.T) {
	source := []byte(`{"value":7,"items":[{"n":1},{"n":2}]}`)
	doc, err := tql.Parse(source, "json")
	if err != nil {
		t.Fatal(err)
	}
	program := `let $count = 0; let $copy = items; if true { let $count = 100; $count += 1; $count; } for $item in items[] { let $before = $item.n; $item.n = .n + $.value; $count += 1; $before; } $copy[0].n = 99; del($copy[1]); $copy[1] = {n: 50}; [$count, $copy, items]`
	result, err := doc.Exec(program)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"101", "1", "2", `[2,[{"n":99},{"n":50}],[{"n":8},{"n":9}]]`}
	if len(result.Values) != len(want) {
		t.Fatalf("values=%v", result.Values)
	}
	for i, v := range result.Values {
		got, err := jsonValue(v)
		if err != nil {
			t.Fatal(err)
		}
		if got != want[i] {
			t.Fatalf("value %d: got %s want %s", i, got, want[i])
		}
	}
	if string(result.Bytes) != `{"value":7,"items":[{"n":8},{"n":9}]}` {
		t.Fatalf("bytes=%s", result.Bytes)
	}
	if _, err := doc.Exec(`$count`); err == nil {
		t.Fatal("variables leaked between executions")
	}
}
func jsonValue(v any) (string, error) { return syntax.JSON(v) }

func TestScriptIterationReferences(t *testing.T) {
	tests := []struct{ name, source, program, want string }{
		{"delete queued sibling", `{"a":[{"v":1},{"v":2},{"v":3}]}`, `for $n in a[] { if $n.v == 1 { del($.a[1]); } $n.v += 10; } a`, `[{"v":11},{"v":13}]`},
		{"appends not visited", `{"a":[1,2]}`, `for $n in a[] { $.a[2] = 3; $n += 10; } a`, `[11,12,3]`},
		{"replaced descendants skipped", `{"a":{"x":1,"y":2}}`, `for $n in walk(a) { if type($n) == "number" { if $n == 1 { $.a = {new:5}; } else { $n += 1; } } } a`, `{"new":5}`},
		{"local tree references", `{}`, `let $copy = [1,2,3,4]; for $x in $copy[] { if $x % 2 == 0 { del($x); } else { $x += 10; } } $copy`, `[11,13]`},
		{"array parent reference survives", `{"a":[{"v":[1,2]},{"v":[3,4]}]}`, `for $parent in a[] { for $x in $parent.v[] { if $x % 2 == 0 { del($x); } else { $x += 10; } } $parent.done = true; } a`, `[{"done":true,"v":[11]},{"done":true,"v":[13]}]`},
		{"computed value loop", `{}`, `let $sum = 0; for $x in [1,2,3][] { $x *= 2; $sum += $x; } $sum`, `12`},
		{"current path after index shift", `{"a":[1,2,3]}`, `let $keys = []; for $x in a[] { if $x == 1 { del($x); continue; } $keys += [key($x)]; } $keys`, `[0,1]`},
		{"nested break continue", `{}`, `let $sum = 0; for $x in [1,2][] { for $y in [1,2,3][] { if $y == 1 { continue; } $sum += $x; break; } } $sum`, `3`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := tql.Parse([]byte(tt.source), "json")
			if err != nil {
				t.Fatal(err)
			}
			r, err := doc.Exec(tt.program)
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonValue(r.Values[len(r.Values)-1])
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}
func TestScriptRollbackAndLimits(t *testing.T) {
	source := []byte(`{"n":0,"items":[1,2,3]}`)
	for _, program := range []string{
		`n = 1; let $x = 2; let $x = 3`,
		`n = 1; if true { let $hidden = 1; } n = $hidden`,
		`for $n in items[] { $n += 1; if $n == 3 { $.n = 1 / 0; } }`,
		`n = 1; while true {}`,
		`let $x = 0; n = 1; while $x < 1 { $x += 0; }`,
		`for $n in items[] { del($n); $n += 1; }`,
	} {
		t.Run(program, func(t *testing.T) {
			doc, err := tql.Parse(source, "json")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = doc.Exec(program); err == nil {
				t.Fatal("expected error")
			}
			if !bytes.Equal(doc.Bytes(), source) {
				t.Fatalf("failed script committed: %s", doc.Bytes())
			}
		})
	}
}
func TestScriptCompiledReuse(t *testing.T) {
	p, err := tql.Compile(`let $count = 0; for $x in items[] { $x += 1; $count += 1; } $count`)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 3 {
				doc, e := tql.Parse([]byte(`{"items":[1,2,3]}`), "json")
				if e != nil {
					t.Error(e)
					return
				}
				r, e := doc.Execute(p)
				if e != nil {
					t.Error(e)
					return
				}
				if len(r.Values) != 1 || r.Values[0] != tql.Number("3") || string(r.Bytes) != `{"items":[2,3,4]}` {
					t.Errorf("unexpected result %+v", r)
				}
			}
		}()
	}
	wg.Wait()
}
func TestBatchedWalkAndAliasFallback(t *testing.T) {
	doc, e := tql.Parse([]byte("base: &a {x: 1, y: 2}\ncopy: *a # c\n"), "yaml")
	if e != nil {
		t.Fatal(e)
	}
	r, e := doc.Exec(`walk(copy) | select(type == "number") |= . + 10`)
	if e != nil {
		t.Fatal(e)
	}
	want := "base: &a {x: 1, y: 2}\ncopy: {\"x\":11,\"y\":12} # c\n"
	if string(r.Bytes) != want {
		t.Fatalf("got %q want %q", r.Bytes, want)
	}
}
func TestReferenceRebasingAndStatementSnapshots(t *testing.T) {
	cases := []struct{ name, source, program, want string }{
		{"deleted previous sibling rebases current", `{"a":[1,2,3,4,5]}`,
			`for $n in a[] { if $n == 3 { del($.a[0]); } $n += 10; } a`, `[12,13,14,15]`},
		{"inner loop rebases outer reference", `{"a":[{"n":1},{"n":2},{"n":3}]}`,
			`for $outer in a[] { if $outer.n == 2 { for $inner in $.a[] { if $inner.n == 1 { del($inner); } } $outer.n += 10; } } a`, `[{"n":12},{"n":3}]`},
		{"local nested loop references survive unrelated mutation", `{}`,
			`let $v = {a:[{n:1},{n:2},{n:3}], spare:0}; for $outer in $v.a[] { $v.spare += 1; for $inner in $v.a[] { if $inner.n == 2 { del($inner); } } $outer.n += 10; } $v`, `{"a":[{"n":11},{"n":13}],"spare":2}`},
		{"shadowing preserves reference owner", `{}`,
			`let $v = {a:[{n:1}]}; for $x in $v.a[] { let $v = {other:2}; $x.n += $v.other; } $v`, `{"a":[{"n":3}]}`},
		{"while declarations fresh each iteration", `{}`,
			`let $n = 0; let $sum = 0; while $n < 4 { let $local = $n; $n += 1; if $local == 1 { continue } $sum += $local; if $local == 2 { break } } [$n,$sum]`, `[3,2]`},
		{"parallel RHS uses statement snapshot", `{"a":[1,2,3]}`,
			`a[] = a; a`, `[[1,2,3],[1,2,3],[1,2,3]]`},
		{"existing and created RHS share snapshot", `{"a":[{"x":1},{}]}`,
			`a[].x = a; a`, `[{"x":[{"x":1},{}]},{"x":[{"x":1},{}]}]`},
		{"local root deletion keeps declaration", `{}`,
			`let $v = {x:1}; del($v); $v = {y:2}; $v.y += 3; $v`, `{"y":5}`},
		{"loop context restored before subsequent read", `{"n":7,"a":[{"n":1}]}`,
			`for $outer in a[] { for $inner in [1][] { .; } .n; } .n`, `7`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := tql.Parse([]byte(tc.source), "json")
			if err != nil {
				t.Fatal(err)
			}
			r, err := doc.Exec(tc.program)
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonValue(r.Values[len(r.Values)-1])
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %s; want %s", got, tc.want)
			}
		})
	}
}

func TestRollbackAfterInvalidatedReferences(t *testing.T) {
	source := []byte(`{"a":[{"n":1},{"n":2}],"changed":false}`)
	for _, program := range []string{
		`$.changed = true; for $x in a[] { $.a = []; $x.n += 1; }`,
		`$.changed = true; let $v = {a:[1,2]}; for $x in $v.a[] { $v = {}; $x += 1; }`,
		`$.changed = true; for $x in a[] { del($x); .n = 9; }`,
		`$.changed = true; let $v = {x:1}; for $x in $v.* { del($v); $x += 1; }`,
		`$.changed = true; a[].n = 1 / 0`,
	} {
		t.Run(program, func(t *testing.T) {
			doc, err := tql.Parse(source, "json")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := doc.Exec(program); err == nil {
				t.Fatal("expected invalidated-reference or RHS error")
			}
			if !bytes.Equal(doc.Bytes(), source) {
				t.Fatalf("partial changes committed: %s", doc.Bytes())
			}
			if _, err := doc.Exec(`changed = true`); err != nil {
				t.Fatalf("failed execution poisoned document: %v", err)
			}
		})
	}
}

func TestBatchFallbackFailureRollsBack(t *testing.T) {
	// The batch is semantically invalid. Sequential fallback first replaces b
	// correctly, then the backend corrupts a when asked to write 4. Neither that
	// successful intermediate replacement nor the preceding statement may commit.
	source := []byte(`{"a":2,"b":1,"changed":false}`)
	doc, err := tql.ParseWith(source, wrongValueFormat{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := doc.Exec(`changed = true; "partial read"; walk() | select(type == "number") |= . + 2`)
	if err == nil {
		t.Fatal("faulty backend output was accepted")
	}
	if result != nil {
		t.Fatalf("partial result escaped transaction: %+v", result)
	}
	if !bytes.Equal(doc.Bytes(), source) {
		t.Fatalf("failed batch fallback committed intermediate state: %s", doc.Bytes())
	}
	got, err := doc.Query(`[a,b,changed]`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := jsonValue(got[0])
	if encoded != `[2,1,false]` {
		t.Fatalf("failed batch fallback mutated original tree: %s", encoded)
	}
}
