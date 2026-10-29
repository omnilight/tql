package tql_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestProgramPersistenceAndAssembly(t *testing.T) {
	source := []byte(`{"a":[1,2,3],"items":[{"name":"api","n":1}]}`)
	program, err := tql.Compile(`let $count = 0; for $x in a[] { $x += 1; $count += 1 } $count`)
	if err != nil {
		t.Fatal(err)
	}
	wantDoc, err := tql.Parse(source, "json")
	if err != nil {
		t.Fatal(err)
	}
	want, err := wantDoc.Execute(program)
	if err != nil {
		t.Fatal(err)
	}
	image, err := program.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := tql.LoadProgram(image)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Mutates() {
		t.Fatal("loaded program lost Mutates")
	}
	for name, p := range map[string]*tql.Program{"loaded": loaded} {
		doc, err := tql.Parse(source, "json")
		if err != nil {
			t.Fatal(err)
		}
		got, err := doc.Execute(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(want.Bytes, got.Bytes) || len(want.Values) != len(got.Values) {
			t.Fatalf("%s: result mismatch: %#v %s", name, got.Values, got.Bytes)
		}
	}
	text := program.Disassemble()
	assembled, err := tql.Assemble(text)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	doc, err := tql.Parse(source, "json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := doc.Execute(assembled)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want.Bytes, got.Bytes) {
		t.Fatalf("assembled bytes: %s", got.Bytes)
	}
}

func TestLoadedProgramParameters(t *testing.T) {
	program, err := tql.Compile(`param $delta; a[] += $delta; a`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := program.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := tql.LoadProgram(image)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := tql.Parse([]byte(`{"a":[1,2]}`), "json")
	if err != nil {
		t.Fatal(err)
	}
	result, err := doc.Execute(loaded, tql.WithParams(map[string]any{"delta": 2}))
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Bytes) != `{"a":[3,4]}` {
		t.Fatalf("bytes=%s", result.Bytes)
	}
}

func TestLoadProgramRejectsCorruption(t *testing.T) {
	program, err := tql.Compile(`a`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := program.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	image[len(image)-1] ^= 0xff
	if _, err := tql.LoadProgram(image); err == nil {
		t.Fatal("corrupt image accepted")
	}
}

func TestLoadProgramRejectsTicklessCycle(t *testing.T) {
	data, err := os.ReadFile("internal/bytecode/testdata/tickless-jump.tqlc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tql.LoadProgram(data); err == nil || !strings.Contains(err.Error(), "no TICK") {
		t.Fatalf("tickless image: err=%v", err)
	}
}

func TestAssembledLoopObeysStepLimit(t *testing.T) {
	program, err := tql.Assemble(`.version 1
.scope S0
.code C0 "main" 0
TICK
JUMP 0
HALT
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, program, `{}`); err == nil || !strings.Contains(err.Error(), "execution limit exceeded") {
		t.Fatalf("err=%v", err)
	}
}

func TestSelectProgramPersistence(t *testing.T) {
	source := `{"items":[{"name":"api","port":1},{"name":"web","port":22}]}`
	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "constant", source: `select(true)`},
		{name: "predicate", source: `items[] | select(length(.name) == 3) | .port`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program, err := tql.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := program.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := tql.LoadProgram(image)
			if err != nil {
				t.Fatal(err)
			}
			assembled, err := tql.Assemble(program.Disassemble())
			if err != nil {
				t.Fatal(err)
			}

			want, err := execute(t, program, source)
			if err != nil {
				t.Fatal(err)
			}
			for name, candidate := range map[string]*tql.Program{"loaded": loaded, "assembled": assembled} {
				got, err := execute(t, candidate, source)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if !bytes.Equal(want.Bytes, got.Bytes) || len(want.Values) != len(got.Values) {
					t.Fatalf("%s: result mismatch: %#v %s", name, got.Values, got.Bytes)
				}
			}
		})
	}
}

func TestLargeIndexIsRejected(t *testing.T) {
	for _, source := range []string{
		`xs[4294967296] = 99`,
		`a[.[4294967296] == 10]`,
	} {
		if _, err := tql.Compile(source); err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Fatalf("%s: err=%v", source, err)
		}
	}
}

func TestInRangeIndexMatchesAcrossSources(t *testing.T) {
	program, err := tql.Compile(`xs[2147483647] = 99`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := program.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := tql.LoadProgram(image)
	if err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string]*tql.Program{"compiled": program, "loaded": loaded} {
		if _, err := execute(t, candidate, `{"xs":[10,20]}`); err == nil || !strings.Contains(err.Error(), "out of bounds") {
			t.Fatalf("%s: err=%v", name, err)
		}
	}

	match, err := tql.Compile(`a[.[2] == 20]`)
	if err != nil {
		t.Fatal(err)
	}
	image, err = match.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loadedMatch, err := tql.LoadProgram(image)
	if err != nil {
		t.Fatal(err)
	}
	source := `{"a":[[0,1,20],[0]]}`
	want, err := execute(t, match, source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := execute(t, loadedMatch, source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want.Bytes, got.Bytes) || len(want.Values) != len(got.Values) {
		t.Fatalf("match plan mismatch: %#v %s", got.Values, got.Bytes)
	}
}

// imageStringLimit mirrors the bytecode image string limit enforced by the
// encoder and decoder.
const imageStringLimit = 1 << 20

func TestStringConstantLimitAcrossSources(t *testing.T) {
	atLimit := strings.Repeat("x", imageStringLimit)
	program, err := tql.Compile(`let $s = "` + atLimit + `"; $s`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := program.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := tql.LoadProgram(image)
	if err != nil {
		t.Fatal(err)
	}
	assembled, err := tql.Assemble(program.Disassemble())
	if err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string]*tql.Program{"loaded": loaded, "assembled": assembled} {
		got, err := execute(t, candidate, `{}`)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got.Values) != 1 || got.Values[0] != atLimit {
			t.Fatalf("%s: string constant was not preserved", name)
		}
	}

	overLimit, err := tql.Compile(`let $s = "` + atLimit + `x"; $s`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := overLimit.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("over-limit string: err=%v", err)
	}
}

func execute(t *testing.T, program *tql.Program, source string) (*tql.Result, error) {
	t.Helper()
	doc, err := tql.Parse([]byte(source), "json")
	if err != nil {
		t.Fatal(err)
	}
	return doc.Execute(program)
}

func FuzzProgramRoundTrip(f *testing.F) {
	for _, source := range []string{
		`a`, `a[]`, `let $x = 1; $x`, `for $x in a[] { $x += 1 }`,
		`param $p = 1; a[] += $p`, `services[name == "api"].port`,
	} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 4096 {
			t.Skip()
		}
		p, err := tql.Compile(source)
		if err != nil {
			return
		}
		image, err := p.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := tql.LoadProgram(image)
		if err != nil {
			t.Fatal(err)
		}
		assembled, err := tql.Assemble(loaded.Disassemble())
		if err != nil {
			t.Fatal(err)
		}
		again, err := assembled.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(image, again) {
			t.Fatal("round trip changed the image")
		}
	})
}
