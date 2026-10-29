package tql

import (
	"github.com/vasileknik76/tql/internal/bytecode"
	"github.com/vasileknik76/tql/internal/query"
)

// Program is an immutable compiled TQL program, safe for concurrent reuse.
type Program struct {
	code *bytecode.Program
}

// Compile parses a query or script into an immutable, reusable Program.
func Compile(text string) (*Program, error) {
	p, err := query.Parse(text)
	if err != nil {
		return nil, err
	}
	code, err := bytecode.Compile(p)
	if err != nil {
		return nil, err
	}
	return &Program{code: code}, nil
}

// LoadProgram validates and loads a bytecode image produced by MarshalBinary.
func LoadProgram(data []byte) (*Program, error) {
	code, err := bytecode.Decode(data)
	if err != nil {
		return nil, err
	}
	return &Program{code: code}, nil
}

// MarshalBinary returns the deterministic, versioned bytecode image.
func (p *Program) MarshalBinary() ([]byte, error) {
	if p == nil || p.code == nil {
		return nil, errProgram
	}
	return bytecode.Encode(p.code)
}

// Mutates reports whether the program contains any assignment or deletion,
// including changes to local variables and operations in untaken branches.
func (p *Program) Mutates() bool {
	if p == nil {
		return false
	}
	return p.code != nil && p.code.Mutates
}

// Disassemble returns canonical, re-assemblable bytecode text. Lines beginning
// with # are debug comments and are ignored by Assemble.
func (p *Program) Disassemble() string {
	if p == nil || p.code == nil {
		return ""
	}
	return bytecode.Disassemble(p.code)
}

// Assemble compiles canonical bytecode assembly into a Program.
func Assemble(text string) (*Program, error) {
	code, err := bytecode.Assemble(text)
	if err != nil {
		return nil, err
	}
	return &Program{code: code}, nil
}
