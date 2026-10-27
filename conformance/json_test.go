package conformance_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vasileknik76/tql/formats/json"
	"github.com/vasileknik76/tql/syntax"
)

func TestJSONTestSuite(t *testing.T) {
	const root = "testdata/json/"
	files, err := filepath.Glob(root + "*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 318 {
		t.Fatalf("incomplete JSON corpus: got %d cases, want 318", len(files))
	}
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			source := read(t, file)
			original := bytes.Clone(source)
			tree, err := (json.Format{}).Parse(source)
			if !bytes.Equal(source, original) {
				t.Fatal("parser mutated source")
			}
			// TQL requires unique decoded keys so that query paths are unambiguous.
			duplicate := name == "y_object_duplicated_key.json" || name == "y_object_duplicated_key_and_value.json"
			switch {
			case strings.HasPrefix(name, "n_"), duplicate:
				if err == nil || tree != nil {
					t.Fatal("accepted input that must be rejected")
				}
				if duplicate && !strings.Contains(err.Error(), "duplicate object key") {
					t.Fatalf("expected duplicate-key error, got %v", err)
				}
			case strings.HasPrefix(name, "y_"):
				if err != nil {
					t.Fatal(err)
				}
			case strings.HasPrefix(name, "i_"):
				// Upstream permits either result, but partial trees are never valid.
				if err != nil && tree != nil {
					t.Fatal("returned partial tree on error")
				}
			default:
				t.Fatal("unknown upstream expectation prefix")
			}
			if err == nil {
				if tree == nil {
					t.Fatal("successful parse returned no tree")
				}
				actual, err := syntax.JSON(tree.Interface())
				if err != nil {
					t.Fatal(err)
				}
				compareJSON(t, source, []byte(actual))
			}
		})
	}
}
