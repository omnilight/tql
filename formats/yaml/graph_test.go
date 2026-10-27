package yaml_test

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/formats/yaml"
)

func TestGraphStreamsAndOwnedSource(t *testing.T) {
	source := []byte("%YAML 1.2\n--- &root\n? [true, null]\n: !local \"value\"\n...\n--- second\n")
	original := bytes.Clone(source)
	stream, err := yaml.ParseStream(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(stream.Documents) != 2 {
		t.Fatalf("documents = %d", len(stream.Documents))
	}
	first, second := stream.Documents[0], stream.Documents[1]
	if !first.ExplicitStart || !first.ExplicitEnd || first.Version != "1.2" || !second.ExplicitStart {
		t.Fatalf("document metadata: %#v, %#v", first, second)
	}
	if first.Start != 0 || first.End != second.Start || second.End != len(source) {
		t.Fatalf("document spans = [%d,%d), [%d,%d)", first.Start, first.End, second.Start, second.End)
	}
	root := first.Root
	if root.Kind != yaml.MappingNode || root.Anchor != "root" || len(root.Content) != 2 {
		t.Fatalf("mapping = %#v", root)
	}
	key, value := root.Content[0], root.Content[1]
	if key.Kind != yaml.SequenceNode || len(key.Content) != 2 || key.Content[0].Tag != "tag:yaml.org,2002:bool" || key.Content[1].Tag != "tag:yaml.org,2002:null" {
		t.Fatalf("typed key = %#v", key)
	}
	if value.Kind != yaml.ScalarNode || value.Tag != "!local" || !value.ExplicitTag || value.Value != "value" {
		t.Fatalf("tagged value = %#v", value)
	}
	if value.Start < 0 || value.End > len(source) || !strings.Contains(string(source[value.Start:value.End]), "!local") {
		t.Fatalf("tagged source span = [%d,%d)", value.Start, value.End)
	}
	for i := range source {
		source[i] = 'x'
	}
	returned := stream.Bytes()
	returned[0] = 'x'
	if !bytes.Equal(stream.Bytes(), original) || first.Root.Content[1].Value != "value" {
		t.Fatal("caller mutation changed the owned source or decoded graph")
	}
}

func TestGraphEmptyStreamsAndInvalidLaterDocument(t *testing.T) {
	for _, source := range []string{"", "# comment\n \t\n"} {
		stream, err := yaml.ParseStream([]byte(source))
		if err != nil || stream == nil || len(stream.Documents) != 0 || string(stream.Bytes()) != source {
			t.Fatalf("empty stream %q: %#v, %v", source, stream, err)
		}
		doc, err := tql.Parse([]byte(source), "yaml")
		if err != nil || doc.Value() != nil {
			t.Fatalf("empty query document %q: %#v, %v", source, doc, err)
		}
	}
	stream, err := yaml.ParseStream([]byte("---\n---\n"))
	if err != nil || len(stream.Documents) != 2 {
		t.Fatalf("two empty documents: %#v, %v", stream, err)
	}
	for _, document := range stream.Documents {
		if document.Root.Kind != yaml.ScalarNode || document.Root.Tag != "tag:yaml.org,2002:null" || document.Root.Value != "" {
			t.Fatalf("empty document = %#v", document.Root)
		}
	}
	for _, source := range []string{"--- valid\n--- [unterminated\n", "--- valid\n--- *missing\n", "a: \x00\n"} {
		if stream, err := yaml.ParseStream([]byte(source)); err == nil || stream != nil {
			t.Fatalf("invalid stream returned partial data: %#v, %v", stream, err)
		}
	}
}

func TestGraphAliasesPreserveIdentityAndCycles(t *testing.T) {
	source := "root: &root [*root]\ncopy: *root\n"
	stream, err := yaml.ParseStream([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	content := stream.Documents[0].Root.Content
	sequence, copy := content[1], content[3]
	self := sequence.Content[0]
	if sequence.Kind != yaml.SequenceNode || self.Kind != yaml.AliasNode || self.Alias != sequence || copy.Alias != sequence || self.Value != "root" {
		t.Fatal("alias identity or recursive edge was lost")
	}
	if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil || !strings.Contains(err.Error(), "recursive") {
		t.Fatalf("recursive graph was accepted as a query tree: %#v, %v", doc, err)
	}
	other, err := yaml.ParseStream([]byte(source))
	if err != nil || other.Documents[0].Root.Content[1] == sequence {
		t.Fatal("independent parses share graph nodes")
	}
}

func TestGraphAnchorRebindingIsLexical(t *testing.T) {
	source := "[&same first, *same, &same second, *same]\n"
	stream, err := yaml.ParseStream([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	items := stream.Documents[0].Root.Content
	if items[1].Alias != items[0] || items[3].Alias != items[2] || items[1].Alias == items[3].Alias {
		t.Fatal("anchor rebinding changed an earlier alias target")
	}
	doc, err := tql.Parse([]byte(source), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if want := []any{"first", "first", "second", "second"}; !reflect.DeepEqual(doc.Value(), want) {
		t.Fatalf("query projection = %#v", doc.Value())
	}
	result, err := doc.Exec(". = .")
	if err != nil || result.Changed || string(result.Bytes) != source {
		t.Fatalf("rebinding no-op: %#v, %v", result, err)
	}
}

func TestGraphScalarLexemesAndTags(t *testing.T) {
	for _, tc := range []struct{ source, value, tag, style string }{
		{"0xFF\n", "0xFF", "tag:yaml.org,2002:int", "plain"},
		{"True\n", "True", "tag:yaml.org,2002:bool", "plain"},
		{"\"decoded\\ttext\"\n", "decoded\ttext", "tag:yaml.org,2002:str", "double"},
		{"|\n  text\n", "text\n", "tag:yaml.org,2002:str", "literal"},
		{">\n  text\n", "text\n", "tag:yaml.org,2002:str", "folded"},
		{"!!int \"not an integer\"\n", "not an integer", "tag:yaml.org,2002:int", "double"},
		{"!local 12\n", "12", "!local", "plain"},
		{".inf\n", ".inf", "tag:yaml.org,2002:float", "plain"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			stream, err := yaml.ParseStream([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			node := stream.Documents[0].Root
			if node.Kind != yaml.ScalarNode || node.Value != tc.value || node.Tag != tc.tag || node.Style != tc.style {
				t.Fatalf("scalar = %#v", node)
			}
		})
	}
}

func TestGraphProjectionRejectsUnrepresentableValues(t *testing.T) {
	for _, source := range []string{
		"? [a, b]\n: value\n", ": null key\n\"\": string key\n",
		"value: !local 12\n", "--- first\n--- second\n", ".inf\n", "!!int \"wrong\"\n",
		"1: numeric key\n\"1\": string key\n",
		"same: first\nsame: second\n",
	} {
		t.Run(source, func(t *testing.T) {
			if stream, err := yaml.ParseStream([]byte(source)); err != nil || stream == nil {
				t.Fatalf("syntax graph rejected: %#v, %v", stream, err)
			}
			if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
				t.Fatalf("query projection accepted: %#v, %v", doc, err)
			}
		})
	}
}

func TestGraphProjectionKeepsStringKeyPolicyAndAliases(t *testing.T) {
	source := "01: one\nnull: empty\nbase: &a {name: before}\ncopy: *a # keep\n"
	doc, err := tql.Parse([]byte(source), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	values := doc.Value().(map[string]any)
	if values["01"] != "one" || values["null"] != "empty" {
		t.Fatalf("primitive key spellings changed: %#v", values)
	}
	result, err := doc.Exec(`copy.name = "after"`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(source, "copy: *a", `copy: {"name":"after"}`, 1)
	if string(result.Bytes) != want {
		t.Fatalf("alias edit changed neighboring bytes: %q", result.Bytes)
	}
	before := doc.Bytes()
	if result, err := doc.Exec(`copy.name = "temporary"; 1 / 0`); err == nil || result != nil || !bytes.Equal(before, doc.Bytes()) {
		t.Fatalf("failed graph projection transaction: %#v, %v", result, err)
	}
}

func TestGraphProjectionKeepsPrimitiveKeyLexemes(t *testing.T) {
	for _, tc := range []struct{ source, key string }{
		{"01: value\n", "01"},
		{"NULL: value\n", "NULL"},
		{"!!int '01': value\n", "01"},
		{"&key 01: value\n", "01"},
		{"!!bool 'True': value\n", "True"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			doc, err := tql.Parse([]byte(tc.source), "yaml")
			if err != nil {
				t.Fatal(err)
			}
			if want := map[string]any{tc.key: "value"}; !reflect.DeepEqual(doc.Value(), want) {
				t.Fatalf("primitive key projection = %#v; want %#v", doc.Value(), want)
			}
		})
	}
}

func TestGraphAliasesStayCompactAndQueryExpansionIsBounded(t *testing.T) {
	var source strings.Builder
	source.WriteString("a0: &a0 [0]\n")
	for i := 1; i <= 18; i++ {
		fmt.Fprintf(&source, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	stream, err := yaml.ParseStream([]byte(source.String()))
	if err != nil || len(stream.Documents[0].Root.Content) != 38 {
		t.Fatalf("compact alias graph: %#v, %v", stream, err)
	}
	if doc, err := tql.Parse([]byte(source.String()), "yaml"); err == nil || doc != nil || !strings.Contains(err.Error(), "alias expansion limit") {
		t.Fatalf("query expansion was not bounded: %#v, %v", doc, err)
	}
}

func TestGraphSameLinePropertyRecursionIsBounded(t *testing.T) {
	for _, property := range []string{"!!str ", "&anchor "} {
		source := strings.Repeat(property, 300) + "value"
		if stream, err := yaml.ParseStream([]byte(source)); err == nil || stream != nil || !strings.Contains(err.Error(), "property nesting limit exceeded") {
			t.Fatalf("unbounded properties: %#v, %v", stream, err)
		}
	}
	stream, err := yaml.ParseStream([]byte("&anchor !!str value"))
	if err != nil || stream.Documents[0].Root.Value != "value" {
		t.Fatalf("valid properties: %#v, %v", stream, err)
	}
}

func FuzzGraph(f *testing.F) {
	for _, source := range []string{"", "---\n---\n", "? [true, null]\n: !local value\n", "&node [*node]", "&a: key: &a value\nfoo: *a:\n", "- ? : x\n", "%TAG !e! tag:example.com,2000:\n--- !e!type value\n"} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		original := bytes.Clone(source)
		stream, err := yaml.ParseStream(source)
		if !bytes.Equal(source, original) {
			t.Fatal("parser changed source")
		}
		if err != nil {
			if stream != nil {
				t.Fatal("partial stream on error")
			}
			return
		}
		if stream == nil || !bytes.Equal(stream.Bytes(), original) {
			t.Fatal("stream source differs")
		}
		seen := map[*yaml.Node]bool{}
		var pending []*yaml.Node
		for _, doc := range stream.Documents {
			if doc.Start < 0 || doc.End < doc.Start || doc.End > len(source) {
				t.Fatalf("invalid document span [%d,%d)", doc.Start, doc.End)
			}
			pending = append(pending, doc.Root)
		}
		for len(pending) > 0 {
			node := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if node == nil {
				t.Fatal("nil graph node")
			}
			if seen[node] {
				continue
			}
			seen[node] = true
			if node.Start < 0 || node.End < node.Start || node.End > len(source) {
				t.Fatalf("invalid node span [%d,%d)", node.Start, node.End)
			}
			if node.Kind == yaml.MappingNode && len(node.Content)%2 != 0 {
				t.Fatal("incomplete mapping entry")
			}
			if node.Kind == yaml.AliasNode {
				if node.Alias == nil {
					t.Fatal("alias has no target")
				}
				pending = append(pending, node.Alias)
			}
			pending = append(pending, node.Content...)
		}
	})
}

func TestDuplicatePendingPropertiesAreRejected(t *testing.T) {
	for _, source := range []string{"&0 &0 !", "&a &b !!str", "!first &a !second", "!!str &a !!str"} {
		if stream, err := yaml.ParseStream([]byte(source)); err == nil || stream != nil {
			t.Fatalf("duplicate properties accepted: %q, %#v, %v", source, stream, err)
		}
		if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
			t.Fatalf("duplicate properties projected: %q, %#v, %v", source, doc, err)
		}
	}
	for _, source := range []string{"&a !!str", "!!str &a"} {
		doc, err := tql.Parse([]byte(source), "yaml")
		if err != nil || doc.Value() != "" {
			t.Fatalf("valid pending properties: %q, %#v, %v", source, doc, err)
		}
	}
}
