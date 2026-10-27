// Package edit builds structured, source-preserving document changes.
// Set and Del construct commands, Diff and Merge plan patches from documents,
// and Document.Apply executes commands atomically. Patch.TQL exports runnable
// source when the values can be represented by the TQL language.
package edit

import (
	"fmt"
	"strings"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/internal/editmodel"
	"github.com/vasileknik76/tql/syntax"
)

// Path addresses literal object keys and nonnegative array indexes. Its zero
// value is the root. Extending a Path never changes the original.
type Path = syntax.Path

// Root returns the document root path.
func Root() Path { return syntax.Root() }

// Key returns a path through literal object keys, starting at the root.
func Key(keys ...string) Path { return syntax.Key(keys...) }

// Operation is an immutable edit command. Its zero value is invalid.
type Operation = syntax.Operation

// Kind identifies an edit command.
type Kind = syntax.OperationKind

const (
	// SetKind assigns a value.
	SetKind = syntax.SetOperation
	// DelKind deletes an object member or array element.
	DelKind = syntax.DelOperation
)

// Set captures a detached snapshot of value and constructs an assignment.
// Missing or null parents are created; sparse arrays and incompatible parents
// fail. Invalid values and paths are reported by Document.Apply or Patch.TQL.
func Set(path Path, value any) Operation { return syntax.Set(path, value) }

// Del constructs a deletion. Missing targets are no-ops. Deleting the root is an
// error reported by Document.Apply or Patch.TQL.
func Del(path Path) Operation { return syntax.Del(path) }

// Patch is an ordered sequence of commands, applied with doc.Apply(patch...).
// Its commands are immutable; the slice itself must not be modified concurrently.
type Patch []Operation

// TQL exports deterministic, runnable TQL source, with a final newline. An empty
// patch exports a no-op. Typed TOML dates and non-finite numbers have no TQL
// literals and return an error, even when nested. No partial source is returned.
func (p Patch) TQL() (string, error) {
	if len(p) == 0 {
		return "if true {}\n", nil
	}
	var b strings.Builder
	for i, op := range p {
		err := editmodel.Validate(op)
		var value string
		if err == nil && op.Kind() == SetKind {
			v := op.Value()
			err = checkLiteral(v)
			if err == nil {
				value, err = syntax.JSON(v)
			}
		}
		if err != nil {
			return "", fmt.Errorf("operation %d (%s %s): %w", i, op.Kind(), op.Path(), err)
		}
		if op.Kind() == DelKind {
			fmt.Fprintf(&b, "del(%s);\n", op.Path())
		} else {
			fmt.Fprintf(&b, "%s = %s;\n", op.Path(), value)
		}
	}
	text := b.String()
	if _, err := tql.Compile(text); err != nil {
		return "", fmt.Errorf("export TQL: %w", err)
	}
	return text, nil
}

func checkLiteral(v any) error {
	switch x := v.(type) {
	case tql.Temporal:
		return fmt.Errorf("TQL has no literal for a TOML date/time")
	case tql.Number:
		if x == "inf" || x == "+inf" || x == "-inf" || x == "nan" || x == "+nan" || x == "-nan" {
			return fmt.Errorf("TQL has no literal for non-finite number %q", x)
		}
	case map[string]any:
		for _, v := range x {
			if err := checkLiteral(v); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range x {
			if err := checkLiteral(v); err != nil {
				return err
			}
		}
	}
	return nil
}
