package yaml

import (
	"reflect"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestTagURIResolution(t *testing.T) {
	for _, tc := range []struct {
		source, uri string
		handles     map[string]string
	}{
		{"!!str", "tag:yaml.org,2002:str", nil},
		{"!local", "!local", nil},
		{"!", "!", nil},
		{"!<tag:example.com,2000:app/foo>", "tag:example.com,2000:app/foo", nil},
		{"!e!tag%21", "tag:example.com,2000:app/tag!", map[string]string{"!e!": "tag:example.com,2000:app/"}},
		{"!e!%E2%98%83", "tag:example.com,2000:☃", map[string]string{"!e!": "tag:example.com,2000:"}},
		{"!!int", "tag:example.com,2000:int", map[string]string{"!!": "tag:example.com,2000:"}},
		{"!e!suffix", "!prefix!suffix", map[string]string{"!e!": "!prefix%21"}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			p := &parser{tagHandles: tc.handles}
			tag, end, err := p.tagProperty([]byte(tc.source), 0, len(tc.source))
			if err != nil || end != len(tc.source) || tag.uri != tc.uri {
				t.Fatalf("got %#v, end %d, error %v; want %q", tag, end, err, tc.uri)
			}
		})
	}
	for _, source := range []string{"!<>", "!<unfinished", "!<tag:example.com,2000:x>value", "!foo{}", "!{}", "!!", "!missing!value", "!a_b!value", "!a!value!bad", "!%GG", "!%FF", "!%E2%98"} {
		t.Run(source, func(t *testing.T) {
			p := &parser{}
			if tag, _, err := p.tagProperty([]byte(source), 0, len(source)); err == nil {
				t.Fatalf("accepted invalid tag %#v", tag)
			}
		})
	}
}

func TestGraphPreservesCustomTagAndAliasIdentity(t *testing.T) {
	source := "%TAG !e! tag:example.com,2000:app/\n---\nkey: &same !e!scalar%21 12\ncopy: *same\n"
	stream, err := ParseStream([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	root := stream.Documents[0].Root
	value, alias := root.Content[1], root.Content[3]
	if value.Tag != "tag:example.com,2000:app/scalar!" || !value.ExplicitTag || value.Value != "12" || value.Anchor != "same" {
		t.Fatalf("tagged value: %#v", value)
	}
	if alias.Kind != AliasNode || alias.Alias != value {
		t.Fatal("alias lost its target identity")
	}
	if out, err := (Format{}).Parse([]byte(source)); err == nil || out != nil {
		t.Fatal("custom tag was silently discarded by query projection")
	}
}

func TestAnchorNamesMayBeRebound(t *testing.T) {
	source := "[&same first, *same, &same second, *same]"
	stream, err := ParseStream([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	nodes := stream.Documents[0].Root.Content
	if nodes[1].Alias != nodes[0] || nodes[3].Alias != nodes[2] || nodes[1].Alias == nodes[3].Alias {
		t.Fatal("aliases did not retain lexical bindings")
	}
	root := parse(t, source)
	if got, want := root.Interface(), []any{"first", "first", "second", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestNestedAnchorRebinding(t *testing.T) {
	stream, err := ParseStream([]byte("&same [&same inner, *same]"))
	if err != nil {
		t.Fatal(err)
	}
	outer := stream.Documents[0].Root
	inner, alias := outer.Content[0], outer.Content[1]
	if outer.Anchor != "same" || inner.Anchor != "same" || alias.Alias != inner {
		t.Fatal("binding the outer node overwrote the inner anchor binding")
	}
}

func TestGraphPreservesRecursiveAlias(t *testing.T) {
	source := "&self [*self]"
	stream, err := ParseStream([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	root := stream.Documents[0].Root
	if root.Content[0].Alias != root {
		t.Fatal("recursive alias identity was not preserved")
	}
	if n, err := (Format{}).Parse([]byte(source)); err == nil || n != nil {
		t.Fatal("recursive graph should fail tree projection")
	}
}

func TestTagConstructionIsSeparateFromSyntax(t *testing.T) {
	stream, err := ParseStream([]byte("!!int not-an-integer"))
	if err != nil {
		t.Fatal(err)
	}
	root := stream.Documents[0].Root
	if root.Tag != "tag:yaml.org,2002:int" || root.Value != "not-an-integer" {
		t.Fatalf("graph lost native tag or scalar: %#v", root)
	}
	if n, err := (Format{}).Parse([]byte("!!int not-an-integer")); err == nil || n != nil {
		t.Fatal("invalid integer loaded into query tree")
	}
	if got := parse(t, "[! 12, ! [1]]").Interface(); !reflect.DeepEqual(got, []any{"12", []any{syntax.Number("1")}}) {
		t.Fatalf("non-specific tags: %#v", got)
	}
}
