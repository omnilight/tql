package edit

import (
	"fmt"
	"slices"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/internal/editmodel"
	"github.com/vasileknik76/tql/internal/query"
)

// Diff plans changes that make from semantically equal to to, preserving from's
// unaffected source. It modifies neither document. Arrays are compared by index;
// object keys are visited in lexical order. Format support is checked on Apply.
func Diff(from, to *tql.Document) (Patch, error) {
	a, err := snapshot(from)
	if err != nil {
		return nil, fmt.Errorf("diff source: %w", err)
	}
	b, err := snapshot(to)
	if err != nil {
		return nil, fmt.Errorf("diff target: %w", err)
	}
	return plannedPatch(a, b)
}

func plannedPatch(before, after any) (Patch, error) {
	var patch Patch
	diff(&patch, Root(), before, after)
	for i, op := range patch {
		if err := editmodel.Validate(op); err != nil {
			return nil, fmt.Errorf("operation %d (%s %s): %w", i, op.Kind(), op.Path(), err)
		}
	}
	return patch, nil
}

func snapshot(doc *tql.Document) (any, error) {
	// An empty Apply validates initialization without changing document state.
	if _, err := doc.Apply(); err != nil {
		return nil, err
	}
	return doc.Value(), nil
}

func diff(patch *Patch, path Path, before, after any) {
	if query.Equal(before, after) {
		return
	}
	if next, ok := after.(map[string]any); ok {
		if prev, ok := before.(map[string]any); ok {
			for _, key := range sortedKeys(prev) {
				if _, exists := next[key]; !exists {
					*patch = append(*patch, Del(path.Key(key)))
				}
			}
			for _, key := range sortedKeys(next) {
				if old, exists := prev[key]; exists {
					diff(patch, path.Key(key), old, next[key])
				} else {
					*patch = append(*patch, Set(path.Key(key), next[key]))
				}
			}
			return
		}
	}
	if next, ok := after.([]any); ok {
		if prev, ok := before.([]any); ok {
			for i := 0; i < min(len(prev), len(next)); i++ {
				diff(patch, path.Index(i), prev[i], next[i])
			}
			for i := len(prev) - 1; i >= len(next); i-- {
				*patch = append(*patch, Del(path.Index(i)))
			}
			for i := len(prev); i < len(next); i++ {
				*patch = append(*patch, Set(path.Index(i), next[i]))
			}
			return
		}
	}
	*patch = append(*patch, Set(path, after))
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
