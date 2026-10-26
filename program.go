package tql

import "github.com/vasileknik76/tql/internal/query"

// Program is an immutable compiled TQL program, safe for concurrent reuse.
type Program struct{ program *query.Program }

// Compile parses a query or script into an immutable, reusable Program.
func Compile(text string) (*Program, error) {
	p, err := query.Parse(text)
	if err != nil {
		return nil, err
	}
	return &Program{program: p}, nil
}

// Mutates reports whether the program contains any assignment or deletion,
// including changes to local variables and operations in untaken branches.
func (p *Program) Mutates() bool { return p != nil && p.program != nil && p.program.Mutates() }
