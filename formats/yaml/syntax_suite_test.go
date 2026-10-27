package yaml_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
	yamlformat "github.com/vasileknik76/tql/formats/yaml"
)

func TestInlineMarkerScalarContent(t *testing.T) {
	for _, source := range []string{"--- ...\n", "--- ... # keep\n", "--- ---\n", "--- --- # keep\n"} {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll(source, "\n", newline)
			t.Run(source, func(t *testing.T) {
				doc, err := tql.Parse([]byte(source), "yaml")
				if err != nil {
					t.Fatal(err)
				}
				want := source[4:7]
				if doc.Value() != want {
					t.Fatalf("got %#v; want %q", doc.Value(), want)
				}
				for range 2 {
					result, err := doc.Exec(`. = "` + want + `"`)
					if err != nil {
						t.Fatal(err)
					}
					if result.Changed || string(result.Bytes) != source {
						t.Fatalf("no-op changed bytes: %q", result.Bytes)
					}
				}
			})
		}
	}
}

func TestPlainSyntaxAndFlowKeys(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   any
	}{
		{"?foo: value\n:foo: value\n-foo: value\nkey:: value\n", map[string]any{"?foo": "value", ":foo": "value", "-foo": "value", "key:": "value"}},
		{"::\n", map[string]any{":": nil}},
		{"?: value\n-: value\n", map[string]any{"?": "value", "-": "value"}},
		{"- single multiline\n - sequence entry\n", []any{"single multiline - sequence entry"}},
		{"- bla\"keks: foo\n- bla]keks: foo\n", []any{map[string]any{"bla\"keks": "foo"}, map[string]any{"bla]keks": "foo"}}},
		{"- :,\n", []any{":,"}},
		{"foo:\n \tbar\n", map[string]any{"foo": "bar"}},
		{"1st non-empty\n\n 2nd non-empty \n\t3rd non-empty\n", "1st non-empty\n2nd non-empty 3rd non-empty"},
		{"x:\n - x\n  \tx\n", map[string]any{"x": []any{"x x"}}},
		{"seq:\n &anchor\n- a\n- b\n", map[string]any{"seq": []any{"a", "b"}}},
		{"{?foo: bar, key?: value?, missing, multi\n line: value}\n", map[string]any{"?foo": "bar", "key?": "value?", "missing": nil, "multi line": "value"}},
		{"{foo\n: bar}\n", map[string]any{"foo": "bar"}},
		{"k: {\n k\n :\n v\n }\n", map[string]any{"k": map[string]any{"k": "v"}}},
		{"\t{}\n", map[string]any{}},
		{"\t[\n\t]\n", []any{}},
		{"[?x]\n", []any{"?x"}},
		{"--- ... text\n", "... text"},
		{"{single line, \"multi\n line\", a: b}\n", map[string]any{"single line": nil, "multi line": nil, "a": "b"}},
	} {
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
				for range 2 {
					result, err := doc.Exec(". = .")
					if err != nil || result.Changed || string(result.Bytes) != source {
						t.Fatalf("no-op: %#v, %v", result, err)
					}
				}
			})
		}
	}
}

func TestInvalidFlowSeparation(t *testing.T) {
	for _, source := range []string{
		"---\n\"\n---\n\"\n", "--- \"a\n... x\nb\"\n", "---\n'\n...\n'\n",
		"---\nflow: [a,\nb,\nc]\n", "---\n[ a, b, c, ]#invalid\n", "---\n[ a, b, c,#invalid\n]\n",
		"---\n- [-, -]\n", "[\n--- ,\n...\n]\n", "key: \"value\"# invalid comment\n",
		"foo: |\n\t\nbar: 1\n", "- [\n\tfoo,\n foo\n ]\n", "-\t-\n", "- \t-\n", "[-]\n",
		"---\n---\n", "---\n--- second\n", "... value\n", "value\n... other\n", "@key: value\n", "`key: value\n",
	} {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll(source, "\n", newline)
			if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
				t.Errorf("accepted invalid input %q", source)
			}
		}
	}
	for _, source := range []string{"flow: [a,\n b,\n c]\n", "[a,b,] # comment\n", "[a,b, # comment\n]\n", "key: \"value\" # comment\n", "['-', '-']\n", "- -\n"} {
		if _, err := tql.Parse([]byte(source), "yaml"); err != nil {
			t.Errorf("valid control %q: %v", source, err)
		}
	}
}

func TestFlowMultilineKeyEdits(t *testing.T) {
	source := "# top\r\nk: {multi\r\n line: old, keep: 'bytes'} # tail\r\n"
	doc, err := tql.Parse([]byte(source), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	result, err := doc.Exec(`k["multi line"] = "new"`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(source, "line: old", "line: \"new\"", 1)
	if string(result.Bytes) != want {
		t.Fatalf("got %q; want %q", result.Bytes, want)
	}
	if got := doc.Value().(map[string]any)["k"].(map[string]any)["multi line"]; got != "new" {
		t.Fatal(got)
	}
}

func TestRootBlockScalarDocumentEnd(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"--- |+\n ab\n \n  \n...\n", "ab\n\n \n"},
		{"--- |-\n ab\n \n \n...\n", "ab"},
		{"|-\n ab\n \n \n...\n", "ab"},
		{"|\n  ---\n  ...\n", "---\n...\n"},
	} {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll(tc.source, "\n", newline)
			doc, err := tql.Parse([]byte(source), "yaml")
			if err != nil {
				t.Fatal(err)
			}
			if doc.Value() != tc.want {
				t.Fatalf("%q: got %#v; want %q", source, doc.Value(), tc.want)
			}
			result, err := doc.Exec(". = .")
			if err != nil || result.Changed || string(result.Bytes) != source {
				t.Fatalf("no-op: %#v, %v", result, err)
			}
		}
	}
	for _, source := range []string{"|\ntext\n---\nsecond\n", "%YAML 1.2\n--- |\n%!PS-Adobe-2.0\n...\n%YAML 1.2\n---\n# Empty\n...\n"} {
		if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
			t.Fatalf("accepted multiple documents %q", source)
		}
	}
}

func TestExplicitKeysAndFlowPairs(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   any
	}{
		{"? a\n? b\nc:\n", map[string]any{"a": nil, "b": nil, "c": nil}},
		{"? a\n  true\n: null\n  d\n? e\n  42\n", map[string]any{"a true": "null d", "e 42": nil}},
		{"items: [foo: bar, ? other: 2, ? missing]\n", map[string]any{"items": []any{map[string]any{"foo": "bar"}, map[string]any{"other": tql.Number("2")}, map[string]any{"missing": nil}}}},
		{"&key name: *key\n", map[string]any{"name": "name"}},
		{"&a: key: &a value\nfoo:\n  *a:\n", map[string]any{"key": "value", "foo": "key"}},
		{"{foo: !!str, !!str : bar}\n", map[string]any{"foo": "", "": "bar"}},
		{"%FOO bar baz # ignored\n%YAML 1.2 # version\n--- value\n", "value"},
	} {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll(tc.source, "\n", newline)
			doc, err := tql.Parse([]byte(source), "yaml")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(doc.Value(), tc.want) {
				t.Fatalf("%q: got %#v; want %#v", source, doc.Value(), tc.want)
			}
			result, err := doc.Exec(". = .")
			if err != nil || result.Changed || string(result.Bytes) != source {
				t.Fatalf("no-op: %#v, %v", result, err)
			}
		}
	}
}

func TestNewMappingFormsRemainEditable(t *testing.T) {
	for _, tc := range []struct{ source, query, want string }{
		{"? a # keep\n? b\n", "a = 3", "? a # keep\n: 3\n? b\n"},
		{"? a", "a = 3", "? a\n: 3"},
		{"? multi\n line\n: old\nkeep: bytes\n", `.["multi line"] = "new"`, "? multi\n line\n: \"new\"\nkeep: bytes\n"},
		{"{foo,bar: 0}\n", "foo = 1", "{foo: 1,bar: 0}\n"},
		{"items: [foo: bar, keep]\n", "items[0].other = 2", "items: [{\"foo\":\"bar\",\"other\":2}, keep]\n"},
		{"items: [foo: bar, keep]\n", "del(items[0].foo)", "items: [{}, keep]\n"},
	} {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			source, want := strings.ReplaceAll(tc.source, "\n", newline), tc.want
			if strings.Contains(tc.source, "\n") {
				want = strings.ReplaceAll(tc.want, "\n", newline)
			}
			doc, err := tql.Parse([]byte(source), "yaml")
			if err != nil {
				t.Fatal(err)
			}
			result, err := doc.Exec(tc.query)
			if err != nil {
				t.Fatalf("%q %s: %v", source, tc.query, err)
			}
			if string(result.Bytes) != want {
				t.Fatalf("got %q; want %q", result.Bytes, want)
			}
			if result, err := doc.Exec(". = ."); err != nil || result.Changed || string(result.Bytes) != want {
				t.Fatalf("repeat: %#v %v", result, err)
			}
		}
	}
}

func TestInvalidCompactMappingTabs(t *testing.T) {
	for _, source := range []string{"?\t-\n", "? -\n:\t-\n", "?\tkey:\n", "? key:\n:\tkey:\n", "-\tkey: value\n"} {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			source := strings.ReplaceAll(source, "\n", newline)
			if stream, err := yamlformat.ParseStream([]byte(source)); err == nil || stream != nil {
				t.Fatalf("accepted invalid compact indentation %q", source)
			}
		}
	}
	for _, source := range []string{"? -\n: -\n", "? key:\n: key:\n", "? a\n: -\tb\n  -  -\tc\n     - d\n"} {
		if _, err := yamlformat.ParseStream([]byte(source)); err != nil {
			t.Fatalf("valid control %q: %v", source, err)
		}
	}
}

func TestExplicitCompactMappingKeyKeepsBothMappings(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		source := "- ? : x" + newline
		stream, err := yamlformat.ParseStream([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		sequence := stream.Documents[0].Root
		if sequence.Kind != yamlformat.SequenceNode || len(sequence.Content) != 1 {
			t.Fatalf("sequence = %#v", sequence)
		}
		outer := sequence.Content[0]
		if outer.Kind != yamlformat.MappingNode || len(outer.Content) != 2 {
			t.Fatalf("outer mapping = %#v", outer)
		}
		key := outer.Content[0]
		if key.Kind != yamlformat.MappingNode || len(key.Content) != 2 {
			t.Fatalf("complex key = %#v", key)
		}
		if key.Content[0].Kind != yamlformat.ScalarNode || key.Content[0].Tag != "tag:yaml.org,2002:null" || key.Content[0].Value != "" || key.Content[1].Value != "x" {
			t.Fatalf("inner mapping = %#v", key.Content)
		}
		if outer.Content[1].Kind != yamlformat.ScalarNode || outer.Content[1].Tag != "tag:yaml.org,2002:null" || outer.Content[1].Value != "" {
			t.Fatalf("outer value = %#v", outer.Content[1])
		}
		if string(stream.Bytes()) != source {
			t.Fatalf("source bytes changed: %q", stream.Bytes())
		}
		if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
			t.Fatal("complex graph must not be flattened into query keys")
		}
	}
}

func TestTagDirectiveURIsAreValidatedWhenUnused(t *testing.T) {
	for _, source := range []string{"%TAG !e! %GG\n--- x\n", "%TAG !e! tag:example.com,%\n--- x\n"} {
		if stream, err := yamlformat.ParseStream([]byte(source)); err == nil || stream != nil {
			t.Fatalf("accepted invalid unused tag URI %q", source)
		}
		if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
			t.Fatalf("query accepted invalid unused tag URI %q", source)
		}
	}
	source := "%TAG !e! tag:example.com,%25:\n--- x\n"
	if doc, err := tql.Parse([]byte(source), "yaml"); err != nil || doc.Value() != "x" {
		t.Fatalf("valid unused directive: %v %v", doc, err)
	}
	source = "%TAG !e! tag:example.com,%25:\n--- !e!kind x\n"
	stream, err := yamlformat.ParseStream([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if got := stream.Documents[0].Root.Tag; got != "tag:example.com,%:kind" {
		t.Fatalf("tag URI decoded more than once: %q", got)
	}
	if string(stream.Bytes()) != source {
		t.Fatal("source bytes changed")
	}
}

func TestAnchorSeparationBeforeFlowCollections(t *testing.T) {
	for _, source := range []string{"&a[1]\n", "&a{x: y}\n", "[&a[1], *a]\n"} {
		if stream, err := yamlformat.ParseStream([]byte(source)); err == nil || stream != nil {
			t.Fatalf("accepted unseparated anchor %q", source)
		}
		if doc, err := tql.Parse([]byte(source), "yaml"); err == nil || doc != nil {
			t.Fatalf("query accepted unseparated anchor %q", source)
		}
	}
	for _, source := range []string{"&a [1]\n", "&a {x: y}\n", "[&a,*a]\n", "[&a\"name, *a\"name]\n"} {
		doc, err := tql.Parse([]byte(source), "yaml")
		if err != nil {
			t.Fatalf("valid anchor %q: %v", source, err)
		}
		result, err := doc.Exec(". = .")
		if err != nil || result.Changed || string(result.Bytes) != source {
			t.Fatalf("no-op %q: %#v %v", source, result, err)
		}
	}
}

func TestComplexKeysShareBlockFlowDepth(t *testing.T) {
	for _, tc := range []struct {
		arrays     int
		key, valid bool
	}{
		{55, true, true}, {56, true, false}, {56, false, true}, {57, false, false},
	} {
		var source strings.Builder
		for i := range 200 {
			source.WriteString(strings.Repeat("  ", i) + "a:\n")
		}
		source.WriteString(strings.Repeat("  ", 200) + strings.Repeat("[", tc.arrays) + "key" + strings.Repeat("]", tc.arrays))
		if tc.key {
			source.WriteString(": end")
		}
		source.WriteByte('\n')
		stream, err := yamlformat.ParseStream([]byte(source.String()))
		if !tc.valid {
			if err == nil || stream != nil || !strings.Contains(err.Error(), "nesting limit exceeded") {
				t.Fatalf("arrays=%d, key=%t: expected depth rejection; partial=%t error %v", tc.arrays, tc.key, stream != nil, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("arrays=%d, key=%t: %v", tc.arrays, tc.key, err)
		}
		node := stream.Documents[0].Root
		for range 200 {
			if node.Kind != yamlformat.MappingNode || len(node.Content) != 2 || node.Content[0].Value != "a" {
				t.Fatalf("unexpected block mapping %#v", node)
			}
			node = node.Content[1]
		}
		if tc.key {
			if node.Kind != yamlformat.MappingNode || len(node.Content) != 2 || node.Content[1].Value != "end" {
				t.Fatalf("unexpected complex-key mapping %#v", node)
			}
			node = node.Content[0]
		}
		for range tc.arrays {
			if node.Kind != yamlformat.SequenceNode || len(node.Content) != 1 {
				t.Fatalf("unexpected array %#v", node)
			}
			node = node.Content[0]
		}
		if node.Kind != yamlformat.ScalarNode || node.Value != "key" {
			t.Fatalf("unexpected leaf %#v", node)
		}
		if string(stream.Bytes()) != source.String() {
			t.Fatal("source bytes changed")
		}
		if !tc.key {
			doc, err := tql.Parse([]byte(source.String()), "yaml")
			if err != nil {
				t.Fatal(err)
			}
			result, err := doc.Exec(". = .")
			if err != nil || result.Changed || string(result.Bytes) != source.String() {
				t.Fatalf("value control no-op: %#v %v", result, err)
			}
		}
	}
}
