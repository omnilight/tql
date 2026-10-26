package yaml_test

import (
	"testing"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/formats/yaml"
)

func TestNativeScalarTagsCannotDiscardCollections(t *testing.T) {
	for _, tag := range []string{"str", "int", "float", "bool", "null"} {
		for _, body := range []string{"{a: b}", "[1]", "{}", "[]"} {
			source := "!!" + tag + " " + body
			t.Run(source, func(t *testing.T) {
				graph, err := yaml.ParseStream([]byte(source))
				if err != nil {
					t.Fatal(err)
				}
				if root := graph.Documents[0].Root; root.Kind != yaml.MappingNode && root.Kind != yaml.SequenceNode {
					t.Fatalf("collection lost in syntax graph: %#v", root)
				}
				if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
					t.Fatalf("invalid scalar projection discarded collection: %#v, %v", doc, err)
				}
			})
		}
	}
	for _, source := range []string{"!!null", "!!null ''", "!!null null"} {
		doc, err := tql.Parse([]byte(source), "yaml")
		if err != nil || doc.Value() != nil {
			t.Fatalf("valid null scalar %q: %#v, %v", source, doc, err)
		}
	}
}

func TestEmptyTypedNullKeyCannotBecomeStringKey(t *testing.T) {
	source := "? !!null ''\n: value\n"
	graph, err := yaml.ParseStream([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	key := graph.Documents[0].Root.Content[0]
	if key.Tag != "tag:yaml.org,2002:null" || key.Value != "" {
		t.Fatalf("null key lost: %#v", key)
	}
	if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
		t.Fatalf("null key became a string key: %#v, %v", doc, err)
	}
	doc, err := tql.Parse([]byte("'': value\n"), "yaml")
	if err != nil || doc.Value().(map[string]any)[""] != "value" {
		t.Fatalf("empty string key control: %#v, %v", doc, err)
	}
}
