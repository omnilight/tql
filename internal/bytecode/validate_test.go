package bytecode

import (
	"os"
	"strings"
	"testing"

	"github.com/vasileknik76/tql/internal/query"
)

func TestAssembleRejectsTicklessCycle(t *testing.T) {
	_, err := Assemble(`.version 1
.scope S0
.code C0 "main" 0
JUMP 0
HALT
`)
	if err == nil || !strings.Contains(err.Error(), "no TICK") {
		t.Fatalf("tickless cycle: err=%v", err)
	}
}

func TestAssembleAcceptsTickingCycle(t *testing.T) {
	if _, err := Assemble(`.version 1
.scope S0
.code C0 "main" 0
TICK
JUMP 0
HALT
`); err != nil {
		t.Fatal(err)
	}
}

func TestAssembleRejectsOutOfRangeIndexes(t *testing.T) {
	tests := map[string]string{
		"path step": `.version 1
.scope S0
.path P0
.step index 4294967296
.code C0 "main" 0
HALT
`,
		"match plan": `.version 1
.scope S0
.match M0 index=4294967296 truth
.code C0 "main" 0
HALT
`,
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Assemble(text); err == nil || !strings.Contains(err.Error(), "out of range") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestDecodeRejectsTicklessCycle(t *testing.T) {
	// The fixture is the v1 image of JUMP 0; HALT produced before cycle
	// validation; it reproduces the reported hang.
	data, err := os.ReadFile("testdata/tickless-jump.tqlc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data); err == nil || !strings.Contains(err.Error(), "no TICK") {
		t.Fatalf("tickless image: err=%v", err)
	}
}

func TestSelectProgramRoundTrip(t *testing.T) {
	p := compiled(t, `select(true)`)
	image, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(image); err != nil {
		t.Fatal(err)
	}
	if _, err := Assemble(Disassemble(p)); err != nil {
		t.Fatalf("assembly round trip: %v", err)
	}
}

func TestEncodeRejectsOutOfRangePathOperands(t *testing.T) {
	t.Run("index", func(t *testing.T) {
		p := compiled(t, `xs[0] = 99`)
		step := findStep(t, p, StepIndex)
		step.Index = maxInt32 + 1
		if _, err := Encode(p); err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Fatalf("out-of-range index: err=%v", err)
		}
	})
	t.Run("slice start", func(t *testing.T) {
		p := compiled(t, `xs[0:2]`)
		step := findStep(t, p, StepSlice)
		step.From = minInt32 - 1
		if _, err := Encode(p); err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Fatalf("out-of-range slice start: err=%v", err)
		}
	})
	t.Run("slice end", func(t *testing.T) {
		p := compiled(t, `xs[0:2]`)
		step := findStep(t, p, StepSlice)
		step.To = maxInt32 + 1
		if _, err := Encode(p); err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Fatalf("out-of-range slice end: err=%v", err)
		}
	})
}

func TestCompileClampsNegativeIndexToImageRange(t *testing.T) {
	p := compiled(t, `xs[-9223372036854775808]`)
	step := findStep(t, p, StepIndex)
	if step.Index != minInt32 {
		t.Fatalf("index=%d; want %d", step.Index, minInt32)
	}
	image, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(image); err != nil {
		t.Fatal(err)
	}
}

func TestCompileRejectsIndexAboveImageRange(t *testing.T) {
	for _, source := range []string{
		`xs[4294967296] = 99`,
		`a[.[4294967296] == 10]`,
	} {
		t.Run(source, func(t *testing.T) {
			ast, err := query.Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Compile(ast); err == nil || !strings.Contains(err.Error(), "out of range") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestEncodeStringLengthLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"at limit", maxNameLength, false},
		{"over limit", maxNameLength + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := compiled(t, `let $s = "x"; $s`)
			value := strings.Repeat("x", tc.size)
			for i := range p.Consts {
				if _, ok := p.Consts[i].Value.(string); ok {
					p.Consts[i].Value = value
				}
			}
			image, err := Encode(p)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "exceeds") {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := Decode(image)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, c := range got.Consts {
				if s, ok := c.Value.(string); ok && len(s) == tc.size {
					found = true
				}
			}
			if !found {
				t.Fatalf("decoded constant of length %d not found", tc.size)
			}
			if _, err := Assemble(Disassemble(got)); err != nil {
				t.Fatalf("assembly round trip: %v", err)
			}
		})
	}
}

func findStep(t *testing.T, p *Program, kind StepKind) *Step {
	t.Helper()
	for i := range p.Paths {
		for j := range p.Paths[i].Steps {
			if p.Paths[i].Steps[j].Kind == kind {
				return &p.Paths[i].Steps[j]
			}
		}
	}
	t.Fatalf("step kind %d not found", kind)
	return nil
}
