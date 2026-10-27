package tql

import (
	"fmt"

	"github.com/vasileknik76/tql/syntax"
)

// Script state is private to one execution. Compiled programs contain only
// bytecode and immutable defaults, and can therefore be reused concurrently.
type binding struct {
	value    *syntax.Node
	ref      *nodeRef
	readonly string
}

func writable(owner *binding) error {
	if owner != nil && owner.readonly != "" {
		return fmt.Errorf("parameter $%s is read-only", owner.readonly)
	}
	return nil
}

type nodeRef struct {
	owner *binding
	path  address
	alive bool
}

const scriptStepLimit = 100000

// documentState is the ownership and reference surface shared by the VM and
// document edits. Edits do not depend on the bytecode interpreter.
type documentState interface {
	root(*binding) *syntax.Node
	value(*binding) *syntax.Node
	owner(*syntax.Node) (*binding, error)
	rebase(*binding, string, address)
}
