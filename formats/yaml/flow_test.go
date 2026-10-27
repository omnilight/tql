package yaml_test

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestFlowPlainContinuation(t *testing.T) {
	cases := []struct {
		source string
		want   any
	}{
		{"a: [one\n  two]\n", map[string]any{"a": []any{"one two"}}},
		{"a: [one \t\n  two\n\n  three, four]\n", map[string]any{"a": []any{"one two\nthree", "four"}}},
		{"a: {b: one\n  two, c: three}\n", map[string]any{"a": map[string]any{"b": "one two", "c": "three"}}},
		{"a: [one\n  # comment\n  , two]\n", map[string]any{"a": []any{"one", "two"}}},
		{"a: [one\n  two # comment\n  , three]\n", map[string]any{"a": []any{"one two", "three"}}},
		{"a: [one\n]\n", map[string]any{"a": []any{"one"}}},
		{"a: [one\n  two#literal]\n", map[string]any{"a": []any{"one two#literal"}}},
		{"a: [one\n  --- two]\n", map[string]any{"a": []any{"one --- two"}}},
		{"a: [one\n  ... two]\n", map[string]any{"a": []any{"one ... two"}}},
		{"a: [\"one\n  --- two\"]\n", map[string]any{"a": []any{"one --- two"}}},
		{"a: [true, 12]\n", map[string]any{"a": []any{true, tql.Number("12")}}},
	}
	for _, tc := range cases {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll(tc.source, "\n", newline)
			t.Run(source, func(t *testing.T) {
				doc, err := tql.Parse([]byte(source), "yaml")
				if err != nil {
					t.Fatal(err)
				}
				if got := doc.Value(); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("got %#v; want %#v", got, tc.want)
				}
				result, err := doc.Exec("a = a")
				if err != nil || result.Changed || string(result.Bytes) != source {
					t.Fatalf("no-op changed source: %#v, %v", result, err)
				}
			})
		}
	}
}

func TestFlowPlainContinuationEdits(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := strings.ReplaceAll("# header\na: [one \t\n  two, keep] # tail\nz: 1\n", "\n", newline)
		doc, err := tql.Parse([]byte(source), "yaml")
		if err != nil {
			t.Fatal(err)
		}
		result, err := doc.Exec(`a[0] |= . + "!"`)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.ReplaceAll("# header\na: [\"one two!\" \t\n  , keep] # tail\nz: 1\n", "\n", newline)
		if string(result.Bytes) != want {
			t.Fatalf("got %q; want %q", result.Bytes, want)
		}
		if got := doc.Value().(map[string]any)["a"].([]any)[0]; got != "one two!" {
			t.Fatalf("edited value = %#v", got)
		}
		for range 2 {
			result, err = doc.Exec(`a[0] = "one two!"`)
			if err != nil || result.Changed || !bytes.Equal(result.Bytes, []byte(want)) {
				t.Fatalf("repeated assignment: %#v, %v", result, err)
			}
		}
	}
}

func TestInvalidFlowPlainContinuation(t *testing.T) {
	for _, source := range []string{
		"a: [one # comment\n  two]\n",
		"a: [one\n  # comment\n  two]\n",
		"a: [one\n  two [three]]\n",
		"a:\n  b: [one\n two]\n",
		"a: [one\n  two: three]\n",
		"[one\n--- two]\n",
		"[one\n... two]\n",
	} {
		doc, err := tql.Parse([]byte(source), "yaml")
		if err == nil || doc != nil {
			t.Errorf("accepted invalid flow %q: %v", source, err)
		}
	}
}

func TestMixedBlockFlowDepth(t *testing.T) {
	for _, tc := range []struct {
		block, flow int
		empty       bool
		valid       bool
	}{
		{0, 256, false, true}, {0, 257, false, false},
		{0, 256, true, true}, {0, 257, true, false},
		{200, 56, false, true}, {200, 57, false, false},
		{200, 56, true, true}, {200, 57, true, false},
		{200, 100, false, false}, {257, 0, false, false},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			var source strings.Builder
			for i := range tc.block {
				source.WriteString(strings.Repeat("  ", i) + "k:\n")
			}
			source.WriteString(strings.Repeat("  ", tc.block))
			source.WriteString(strings.Repeat("[", tc.flow))
			if !tc.empty {
				source.WriteString("0")
			}
			source.WriteString(strings.Repeat("]", tc.flow) + "\n")
			doc, err := tql.Parse([]byte(source.String()), "yaml")
			if !tc.valid {
				if err == nil || doc != nil || !strings.Contains(err.Error(), "nesting limit exceeded") {
					t.Fatalf("expected depth rejection: doc %v, error %v", doc, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			value := doc.Value()
			for range tc.block {
				value = value.(map[string]any)["k"]
			}
			for i := range tc.flow {
				items := value.([]any)
				if tc.empty && i == tc.flow-1 {
					if len(items) != 0 {
						t.Fatalf("nonempty leaf: %#v", items)
					}
					return
				}
				value = items[0]
			}
			if value != tql.Number("0") {
				t.Fatalf("leaf = %#v", value)
			}
		})
	}
}

func TestFlowPropertiesShareContainerDepth(t *testing.T) {
	for _, block := range []string{"mapping", "sequence", "compact mapping"} {
		for _, leaf := range []string{"&a 0", "!!str 0", "&a !!str 0", "&a 0, *a"} {
			for _, valid := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/%t", block, leaf, valid), func(t *testing.T) {
					levels, flowLevels := 255, 1
					if block == "compact mapping" {
						levels, flowLevels = 127, 2
					}
					if !valid {
						flowLevels++
					}
					var source strings.Builder
					indent := 0
					for range levels {
						source.WriteString(strings.Repeat(" ", indent))
						switch block {
						case "mapping":
							source.WriteString("k:\n")
							indent += 2
						case "sequence":
							source.WriteString("-\n")
							indent += 2
						case "compact mapping":
							source.WriteString("- k:\n")
							indent += 4
						}
					}
					source.WriteString(strings.Repeat(" ", indent) + strings.Repeat("[", flowLevels) + leaf + strings.Repeat("]", flowLevels) + "\n")
					doc, err := tql.Parse([]byte(source.String()), "yaml")
					if !valid {
						if err == nil || doc != nil || !strings.Contains(err.Error(), "nesting limit exceeded") {
							t.Fatalf("expected depth rejection: doc %v, error %v", doc, err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					result, err := doc.Exec(". = .")
					if err != nil || result.Changed || string(result.Bytes) != source.String() {
						t.Fatalf("boundary no-op: %#v, %v", result, err)
					}
				})
			}
		}
	}
}

func TestFlowPropertyNestingLimit(t *testing.T) {
	for _, count := range []int{1, 257} {
		source := "[" + strings.Repeat("!!str ", count) + "0]\n"
		doc, err := tql.Parse([]byte(source), "yaml")
		if count > 256 {
			if err == nil || doc != nil || !strings.Contains(err.Error(), "property nesting limit exceeded") {
				t.Fatalf("expected property limit: %v, %v", doc, err)
			}
		} else if err != nil || !reflect.DeepEqual(doc.Value(), []any{"0"}) {
			t.Fatalf("valid property chain: %v, %v", doc, err)
		}
	}
}

func TestPropertiesOnNestedFlowCollections(t *testing.T) {
	var source strings.Builder
	for i := range 256 {
		fmt.Fprintf(&source, "&a%d !!seq [", i)
	}
	source.WriteString("0" + strings.Repeat("]", 256) + "\n")
	doc, err := tql.Parse([]byte(source.String()), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	value := doc.Value()
	for range 256 {
		value = value.([]any)[0]
	}
	if value != tql.Number("0") {
		t.Fatalf("leaf = %#v", value)
	}
}
