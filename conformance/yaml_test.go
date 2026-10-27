package conformance_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
	yamlformat "github.com/vasileknik76/tql/formats/yaml"
	"github.com/vasileknik76/tql/syntax"
)

func TestYAMLTestSuite(t *testing.T) {
	const root = "testdata/yaml"
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == "in.yaml" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 402 {
		t.Fatalf("incomplete YAML corpus: got %d cases, want 402", len(files))
	}
	for _, file := range files {
		folder := filepath.Dir(file)
		name, err := filepath.Rel(root, folder)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.ToSlash(name), func(t *testing.T) {
			source := read(t, file)
			original := bytes.Clone(source)
			stream, err := yamlformat.ParseStream(source)
			if !bytes.Equal(source, original) {
				t.Fatal("parser mutated source")
			}
			if exists(t, filepath.Join(folder, "error")) {
				if err == nil || stream != nil {
					t.Fatal("accepted invalid YAML or returned a partial stream")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stream.Bytes(), original) {
				t.Fatal("parse changed source bytes")
			}
			actual, err := events(stream)
			if err != nil {
				t.Fatal(err)
			}
			expected := string(read(t, filepath.Join(folder, "test.event")))
			if actual != expected {
				t.Errorf("events differ:\nwant:\n%s\n got:\n%s", expected, actual)
			}
			companion := filepath.Join(folder, "in.json")
			if exists(t, companion) {
				values := make([]any, len(stream.Documents))
				for i, document := range stream.Documents {
					values[i], err = jsonValue(document.Root, make(map[*yamlformat.Node]bool))
					if err != nil {
						t.Fatal(err)
					}
				}
				actual, err := syntax.JSON(values)
				if err != nil {
					t.Fatal(err)
				}
				// Each companion is a stream of JSON values, one per YAML document.
				expectedValues := jsonDocuments(t, read(t, companion))
				actualValues := jsonDocuments(t, []byte(actual))[0]
				if !reflect.DeepEqual(expectedValues, actualValues) {
					t.Errorf("JSON values differ: want %#v, got %#v", expectedValues, actualValues)
				}
			}
		})
	}
}

var eventEscape = strings.NewReplacer(
	"\\", "\\\\", "\n", "\\n", "\r", "\\r", "\t", "\\t", "\b", "\\b")

func events(stream *yamlformat.Stream) (string, error) {
	var b strings.Builder
	b.WriteString("+STR\n")
	for _, document := range stream.Documents {
		b.WriteString("+DOC")
		if document.ExplicitStart {
			b.WriteString(" ---")
		}
		b.WriteByte('\n')
		if err := nodeEvents(&b, document.Root); err != nil {
			return "", err
		}
		b.WriteString("-DOC")
		if document.ExplicitEnd {
			b.WriteString(" ...")
		}
		b.WriteByte('\n')
	}
	b.WriteString("-STR\n")
	return b.String(), nil
}

func nodeEvents(b *strings.Builder, node *yamlformat.Node) error {
	if node == nil {
		return fmt.Errorf("nil graph node")
	}
	if node.Kind == yamlformat.AliasNode {
		fmt.Fprintf(b, "=ALI *%s\n", node.Value)
		return nil
	}
	properties := ""
	if node.Anchor != "" {
		properties += " &" + node.Anchor
	}
	if node.ExplicitTag {
		properties += " <" + node.Tag + ">"
	}
	switch node.Kind {
	case yamlformat.MappingNode, yamlformat.SequenceNode:
		opening, closing := "+SEQ", "-SEQ"
		if node.Kind == yamlformat.MappingNode {
			opening, closing = "+MAP", "-MAP"
		}
		if node.Style == "flow" {
			if node.Kind == yamlformat.MappingNode {
				opening += " {}"
			} else {
				opening += " []"
			}
		}
		b.WriteString(opening + properties + "\n")
		for _, child := range node.Content {
			if err := nodeEvents(b, child); err != nil {
				return err
			}
		}
		b.WriteString(closing + "\n")
	case yamlformat.ScalarNode:
		style := ":"
		switch node.Style {
		case "single":
			style = "'"
		case "double":
			style = "\""
		case "literal":
			style = "|"
		case "folded":
			style = ">"
		}
		fmt.Fprintf(b, "=VAL%s %s%s\n", properties, style, eventEscape.Replace(node.Value))
	default:
		return fmt.Errorf("unexpected graph kind %q", node.Kind)
	}
	return nil
}

// JSON companions omit YAML metadata; the mandatory event comparison retains it.
func jsonValue(node *yamlformat.Node, active map[*yamlformat.Node]bool) (any, error) {
	if node == nil {
		return nil, fmt.Errorf("nil graph value")
	}
	if active[node] {
		return nil, fmt.Errorf("cyclic value cannot be represented as JSON")
	}
	active[node] = true
	defer delete(active, node)
	if node.Kind == yamlformat.AliasNode {
		return jsonValue(node.Alias, active)
	}
	switch node.Kind {
	case yamlformat.MappingNode:
		if len(node.Content)%2 != 0 {
			return nil, fmt.Errorf("incomplete graph mapping entry")
		}
		result := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key, err := jsonKey(node.Content[i])
			if err != nil {
				return nil, err
			}
			if _, exists := result[key]; exists {
				return nil, fmt.Errorf("mapping keys collide in JSON")
			}
			value, err := jsonValue(node.Content[i+1], active)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		return result, nil
	case yamlformat.SequenceNode:
		result := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := jsonValue(child, active)
			if err != nil {
				return nil, err
			}
			result[i] = value
		}
		return result, nil
	case yamlformat.ScalarNode:
		switch node.Tag {
		case "tag:yaml.org,2002:null":
			return nil, nil
		case "tag:yaml.org,2002:bool", "tag:yaml.org,2002:int", "tag:yaml.org,2002:float":
			// Reuse public native construction; fixture JSON is the value oracle.
			tag := strings.TrimPrefix(node.Tag, "tag:yaml.org,2002:")
			document, err := tql.Parse([]byte("!!"+tag+" "+syntax.Quote(node.Value)), "yaml")
			if err != nil {
				return nil, err
			}
			return document.Value(), nil
		default:
			return node.Value, nil
		}
	default:
		return nil, fmt.Errorf("unexpected graph value kind %q", node.Kind)
	}
}

func jsonKey(node *yamlformat.Node) (string, error) {
	visited := make(map[*yamlformat.Node]bool)
	for node != nil && node.Kind == yamlformat.AliasNode {
		if visited[node] {
			return "", fmt.Errorf("cyclic mapping key")
		}
		visited[node] = true
		node = node.Alias
	}
	if node == nil || node.Kind != yamlformat.ScalarNode {
		return "", fmt.Errorf("complex mapping key cannot be represented as JSON")
	}
	return node.Value, nil
}
