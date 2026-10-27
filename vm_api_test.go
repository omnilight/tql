package tql_test

import (
	"bytes"
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
