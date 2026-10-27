package edit

import (
	"fmt"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/internal/editmodel"
	"github.com/vasileknik76/tql/internal/query"
)

// ArrayMode controls merging when both values are arrays.
type ArrayMode uint8

const (
	// ReplaceArrays replaces the base array with the overlay array (the default).
	ReplaceArrays ArrayMode = iota
	// AppendArrays retains every element, including duplicates, base first.
	AppendArrays
	// UnionArrays preserves the base and appends semantically new overlay elements.
	UnionArrays
)

// MergeOption configures a merge. The zero value is invalid.
type MergeOption struct {
	kind  string
	mode  ArrayMode
	paths []Path
}

// WithArrays selects the mode for all arrays. The last such option wins.
func WithArrays(mode ArrayMode) MergeOption { return MergeOption{kind: "arrays", mode: mode} }

// WithReplace marks overlay subtrees that replace rather than merge with base.
// Paths start at the root and may traverse only overlay objects. Repeated options
// accumulate paths. Unknown paths and traversal into arrays return a Merge error.
func WithReplace(paths ...Path) MergeOption {
	return MergeOption{kind: "replace", paths: append([]Path(nil), paths...)}
}

type replacement struct {
	all      bool
	children map[string]*replacement
}

// Merge plans a patch that overlays incoming values onto base, without modifying
// either document. Objects merge recursively, base-only keys survive, null is a
// value, and other incoming values win. Arrays replace by default. The resulting
// patch edits only semantic differences, including inside replacement subtrees.
func Merge(base, overlay *tql.Document, options ...MergeOption) (Patch, error) {
	a, err := snapshot(base)
	if err != nil {
		return nil, fmt.Errorf("merge base: %w", err)
	}
	b, err := snapshot(overlay)
	if err != nil {
		return nil, fmt.Errorf("merge overlay: %w", err)
	}
	mode := ReplaceArrays
	replace := &replacement{}
	for _, opt := range options {
		switch opt.kind {
		case "arrays":
			if opt.mode > UnionArrays {
				return nil, fmt.Errorf("unknown array mode %d", opt.mode)
			}
			mode = opt.mode
		case "replace":
			for _, path := range opt.paths {
				if err := addReplacement(replace, path, b); err != nil {
					return nil, fmt.Errorf("replace %s: %w", path, err)
				}
			}
		default:
			return nil, fmt.Errorf("invalid merge option")
		}
	}
	desired := merge(a, b, mode, replace)
	return plannedPatch(a, desired)
}

func addReplacement(root *replacement, path Path, overlay any) error {
	if err := editmodel.PathError(path); err != nil {
		return err
	}
	n := root
	for _, step := range editmodel.Steps(path) {
		if step.Array {
			return fmt.Errorf("replacement paths must use object keys")
		}
		obj, ok := overlay.(map[string]any)
		if !ok {
			return fmt.Errorf("cannot traverse non-object overlay")
		}
		v, exists := obj[step.Key]
		if !exists {
			return fmt.Errorf("overlay key %q does not exist", step.Key)
		}
		overlay = v
		if n.children == nil {
			n.children = make(map[string]*replacement)
		}
		if n.children[step.Key] == nil {
			n.children[step.Key] = &replacement{}
		}
		n = n.children[step.Key]
	}
	n.all = true
	return nil
}

func merge(base, overlay any, mode ArrayMode, replace *replacement) any {
	if replace != nil && replace.all {
		return overlay
	}
	switch next := overlay.(type) {
	case map[string]any:
		if prev, ok := base.(map[string]any); ok {
			out := make(map[string]any, len(prev)+len(next))
			for k, v := range prev {
				out[k] = v
			}
			for k, v := range next {
				var child *replacement
				if replace != nil {
					child = replace.children[k]
				}
				if old, exists := prev[k]; exists {
					out[k] = merge(old, v, mode, child)
				} else {
					out[k] = v
				}
			}
			return out
		}
	case []any:
		if prev, ok := base.([]any); ok && mode != ReplaceArrays {
			out := append([]any{}, prev...)
			for _, v := range next {
				if mode == AppendArrays || !contains(out, v) {
					out = append(out, v)
				}
			}
			return out
		}
	}
	return overlay
}

func contains(values []any, value any) bool {
	for _, v := range values {
		if query.Equal(v, value) {
			return true
		}
	}
	return false
}
