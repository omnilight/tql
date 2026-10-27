package tql_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/edit"
	tomlformat "github.com/vasileknik76/tql/formats/toml"
)

func TestGenerationStyle(t *testing.T) {
	tests := []struct {
		name, format, src, query string
		style                    tql.Style
		want                     string
	}{
		{name: "table document", format: "toml", src: "foo = 1\n\n[obj]\nv = 2\n\n[[arr]]\nv = 1\n", query: "obj2.v=1", want: "foo = 1\n\n[obj]\nv = 2\n\n[[arr]]\nv = 1\n\n[obj2]\nv = 1\n"},
		{name: "inline document", format: "toml", src: "obj = { v = 2 }\n", query: "obj2.v=1", want: "obj = { v = 2 }\nobj2 = { v = 1 }\n"},
		{name: "multiline inline document", format: "toml", src: "obj = {\n    v = 1,\n}\n", query: "new.v=2", want: "obj = {\n    v = 1,\n}\nnew = {\n    v = 2,\n}\n"},

		{name: "table majority", format: "toml", src: "inline={v=1}\n[a]\nv=1\n[b]\nv=1\n", query: "new.v=2", want: "inline={v=1}\n[a]\nv=1\n[b]\nv=1\n\n[new]\nv=2\n"},
		{name: "dotted", format: "toml", src: "obj.v=1\n", query: "new.v=2", want: "obj.v=1\nnew.v=2\n"},
		{name: "force inline", format: "toml", src: "[obj]\nv=2\n", query: "new.v=1", style: tql.Style{Objects: "flow"}, want: "new = { v = 1 }\n[obj]\nv=2\n"},
		{name: "force table", format: "toml", src: "obj={v=2}\n", query: "new.v=1", style: tql.Style{Objects: "block"}, want: "obj={v=2}\n\n[new]\nv=1\n"},
		{name: "force dotted", format: "toml", src: "[obj]\nv=2\n", query: "new.a.v=1", style: tql.Style{Objects: "dotted"}, want: "new.a.v = 1\n[obj]\nv=2\n"},
		{name: "nested table in array element", format: "toml", src: "[[arr]]\nv=1\n[[arr]]\nv=2\n", query: "arr[0].child.v=3", style: tql.Style{Objects: "block"}, want: "[[arr]]\nv=1\n\n[arr.child]\nv=3\n[[arr]]\nv=2\n"},
		{name: "new array tables", format: "toml", src: "[[arr]]\nv=1\n", query: "new=[{v:2},{v:3}]", want: "[[arr]]\nv=1\n\n[[new]]\nv=2\n\n[[new]]\nv=3\n"},
		{name: "new inline array", format: "toml", src: "arr=[{v=1}]\n", query: "new=[{v:2}]", want: "arr=[{v=1}]\nnew=[{v=2}]\n"},
		{name: "scalar multiline array", format: "toml", src: "n=1\n", query: "new=[1,2]", style: tql.Style{Arrays: "block", Indent: "    "}, want: "n=1\nnew=[\n    1,\n    2\n]\n"},
		{name: "flow boundary toml", format: "toml", src: "obj={v=1}\n", query: "obj.new.x=2", style: tql.Style{Objects: "block"}, want: "obj={v=1,new={x=2}}\n"},
		{name: "yaml block", format: "yaml", src: "obj:\n  v: 1\n", query: "new.v=2", want: "obj:\n  v: 1\nnew:\n  v: 2\n"},
		{name: "yaml flow", format: "yaml", src: "obj: {v: 1}\n", query: "new.v=2", want: "obj: {v: 1}\nnew: {v: 2}\n"},
		{name: "yaml independent kinds", format: "yaml", src: "obj: {v: 1}\narr:\n  - 1\n", query: "new={v:2,arr:[2,3]}", style: tql.Style{Objects: "flow", Arrays: "block"}, want: "obj: {v: 1}\narr:\n  - 1\nnew: {v: 2,arr: [2,3]}\n"},
		{name: "yaml force flow", format: "yaml", src: "obj:\n  v: 1\n", query: "new={v:2}", style: tql.Style{Objects: "flow", Keys: "single"}, want: "obj:\n  v: 1\n'new': {'v': 2}\n"},
		{name: "yaml string quote", format: "yaml", src: "v: before\n", query: "new=\"true\"", style: tql.Style{Strings: "plain"}, want: "v: before\nnew: \"true\"\n"},
		{name: "toml quote override", format: "toml", src: "v='before'\n", query: "new=\"after\"", style: tql.Style{Strings: "double", Keys: "double"}, want: "v='before'\n\"new\"=\"after\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, e := tql.Parse([]byte(tt.src), tt.format, tql.WithStyle(tt.style))
			if e != nil {
				t.Fatal(e)
			}
			r, e := d.Exec(tt.query)
			if e != nil {
				t.Fatal(e)
			}
			if string(r.Bytes) != tt.want {
				t.Errorf("want %q\n got %q", tt.want, r.Bytes)
			}
		})
	}
}

func TestStyledStringsPreserveSemantics(t *testing.T) {
	values := []string{"", "hello", "true", "1", "null", "a: b", "a # b", "can't", "a\nb", "a\nb\n", "a\nb\n\n", "\nleading", "  indent\n    more\n", "\n\n", "\r\n", "'''", `"""`, "\\n", "\x00\u0085\u2028"}
	for _, format := range []string{"toml", "yaml"} {
		styles := []string{"auto", "single", "double", "literal"}
		if format == "toml" {
			styles = append(styles, "multiline")
		} else {
			styles = append(styles, "plain", "folded")
		}
		for _, style := range styles {
			for _, v := range values {
				t.Run(format+"/"+style+"/"+v, func(t *testing.T) {
					d, e := tql.Parse([]byte("old: before\n"), format, tql.WithStyle(tql.Style{Strings: style}))
					if format == "toml" {
						d, e = tql.Parse([]byte("old='before'\n"), format, tql.WithStyle(tql.Style{Strings: style}))
					}
					if e != nil {
						t.Fatal(e)
					}
					r, e := d.Apply(edit.Set(edit.Key("new"), v))
					if e != nil {
						t.Fatal(e)
					}
					if d.Value().(map[string]any)["new"] != v {
						t.Fatalf("lost value: %q", r.Bytes)
					}
				})
			}
		}
	}
}

func TestStyleAPIAndValidation(t *testing.T) {
	for _, format := range []string{"toml", "yaml"} {
		for _, s := range []tql.Style{{Objects: "bogus"}, {Objects: "block|flow"}, {Strings: "single|double"}, {Arrays: "inline"}, {Keys: "literal"}, {LineEnding: "cr"}, {Indent: "x"}, {Indent: strings.Repeat(" ", 17)}} {
			if _, e := tql.Parse(nil, format, tql.WithStyle(s)); e == nil {
				t.Fatalf("accepted invalid style %+v", s)
			}
		}
	}
	for _, s := range []tql.Style{{Objects: "dotted"}, {Strings: "multiline"}, {Indent: "\t"}} {
		if _, e := tql.Parse(nil, "yaml", tql.WithStyle(s)); e == nil {
			t.Fatalf("accepted YAML style %+v", s)
		}
	}
	if _, e := tql.Parse([]byte("{}"), "json", tql.WithStyle(tql.Style{Objects: "block"})); e == nil {
		t.Fatal("JSON silently ignored style")
	}
	if _, e := tql.Parse(nil, "toml", nil); e == nil {
		t.Fatal("nil option accepted")
	}
	d, e := tql.ParseWith([]byte("old={v=1}\n"), tomlformat.Format{}, tql.WithStyle(tql.Style{Objects: "block"}))
	if e != nil {
		t.Fatal(e)
	}
	before := d.Bytes()
	if _, e = d.Exec("new.v=2; old[0]=3"); e == nil {
		t.Fatal("expected rollback")
	}
	if !bytes.Equal(before, d.Bytes()) {
		t.Fatal("style edit escaped transaction")
	}
	if _, e = d.Apply(edit.Set(edit.Key("new").Key("v"), 1)); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(d.Bytes()), "[new]") {
		t.Fatal("Apply lost style")
	}
	r, e := tql.Eval([]byte("old={v=1}\n"), "toml", "new.v=2", tql.WithStyle(tql.Style{Objects: "block"}))
	if e != nil || !strings.Contains(string(r.Bytes), "[new]") {
		t.Fatalf("Eval lost style: %v", e)
	}
}

func TestStyleScalarsAndBoundaries(t *testing.T) {
	for _, tt := range []struct {
		format, src, query string
		style              tql.Style
		part               string
	}{
		{"yaml", "old: 0xFF\n", "new=254", tql.Style{}, "new: 0xFE\n"},
		{"toml", "old=0b11\n", "new=5", tql.Style{}, "new=0b101\n"},
		{"yaml", "old: TRUE\n", "new=false", tql.Style{}, "new: FALSE\n"},
		{"yaml", "old: ~\n", "new=null", tql.Style{}, "new: ~\n"},
		{"yaml", "old: NULL\n", "new=null", tql.Style{}, "new: NULL\n"},
		{"toml", "old=1\n", "new=-5", tql.Style{Numbers: "hex"}, "new=-5\n"},
		{"toml", "old=1\n", "new=9007199254740993", tql.Style{Numbers: "hex"}, "new=0x20000000000001\n"},
		{"toml", "old=1\n", "new=1.234567890123456789", tql.Style{Numbers: "hex"}, "new=1.234567890123456789\n"},
		{"toml", "obj={v=1}\n", "new=[{v:2}]", tql.Style{}, "new=[{v=2}]\n"},
		{"toml", "obj={v=1}\n", "new=[2,3]", tql.Style{Arrays: "block"}, "new=[\n  2,\n  3\n]\n"},
		{"toml", "[[arr]]\nv=1\n", "arr[1]={child:{v:2},v:3}", tql.Style{Objects: "block"}, "v=3\n\n[arr.child]\nv=2\n"},
		{"toml", "[old]\nv=1\n", `new={"a.b":{v:2}}`, tql.Style{Objects: "block"}, `[new."a.b"]`},
		{"toml", "old=1\n", "new.a.v=2", tql.Style{Objects: "dotted"}, "new.a.v=2\n"},
		{"toml", "[base]\nx=1\n", "base.new={a:{rows:[{v:1}]},z:2}", tql.Style{Objects: "dotted", Arrays: "block"}, "new.z=2\n\n[[base.new.a.rows]]\nv=1\n"},
		{"toml", "a='one'\nb='two'\nc=\"three\"\n", `new="four"`, tql.Style{}, "new='four'\n"},
		{"yaml", "a: 0x11\nb: 0x22\nc: 3\n", "new=15", tql.Style{}, "new: 0xf\n"},
		{"toml", "[obj.child]\nv=1\n", "new.v=2", tql.Style{}, "[new]\n"},
		{"toml", "old=1\n", "new={}", tql.Style{Objects: "dotted"}, "new={ }\n"},
		{"toml", "old=1\n", "new=[]", tql.Style{Arrays: "block"}, "new=[]\n"},
		{"toml", "old=1\n", "new=[{},{}]", tql.Style{Arrays: "block"}, "[[new]]\n\n[[new]]\n"},
		{"yaml", "old:\n  - {v: 1}\nscalar: [1,2]\n", "new=[{v:2}]", tql.Style{}, "new:\n  -"},
		{"toml", "old=1\r\n", "new.v=2", tql.Style{LineEnding: "crlf"}, "\r\n[new]\r\nv=2\r\n"},
		{"yaml", "old: before\n", "new=\"a\\nb\"", tql.Style{Strings: "literal"}, "new: |2-\n  a\n  b\n"},
		{"yaml", "old: before\n", "new=\"a b\"", tql.Style{Strings: "folded"}, "new: >2-\n"},
		{"yaml", "old: before\n", "new=\"a\\nb\\n\"", tql.Style{Strings: "literal", LineEnding: "crlf"}, "new: |2\r\n  a\r\n  b\r\n"},
		{"yaml", "anchor: &a {v: 1}\ncopy: *a\n", "copy.new=\"after\"", tql.Style{Strings: "single"}, "'after'"},
		{"yaml", "\n", "new.v=1", tql.Style{Objects: "block", Keys: "plain"}, "new:\n  v: 1\n"},
	} {
		t.Run(tt.format+"/"+tt.query+"/"+tt.part, func(t *testing.T) {
			d, e := tql.Parse([]byte(tt.src), tt.format, tql.WithStyle(tt.style))
			if e != nil {
				t.Fatal(e)
			}
			r, e := d.Exec(tt.query)
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(string(r.Bytes), tt.part) {
				t.Errorf("want %q in %q", tt.part, r.Bytes)
			}
			r, e = d.Exec(tt.query)
			if e != nil || r.Changed {
				t.Fatalf("not idempotent: %v", e)
			}
		})
	}
}

func FuzzGenerationPolicy(f *testing.F) {
	for i, v := range []string{"a", "true", "a\nb", "a\nb\n\n", "  a\n\n b\n", "\n\n", "can't", "'''", "\"\"\"", "\r\n", "\x00", "\u0085\u2028"} {
		f.Add(uint8(i), v)
	}
	f.Fuzz(func(t *testing.T, seed uint8, v string) {
		if len(v) > 512 || !utf8.ValidString(v) {
			t.Skip()
		}
		for _, format := range []string{"toml", "yaml"} {
			styles := []string{"auto", "single", "double", "literal"}
			if format == "yaml" {
				styles = append(styles, "folded", "plain")
			} else {
				styles = append(styles, "multiline")
			}
			objects := []string{"auto", "block", "flow"}
			if format == "toml" {
				objects = append(objects, "dotted")
			}
			policy := tql.Style{Objects: objects[int(seed)%len(objects)], Arrays: []string{"auto", "flow", "block"}[int(seed/3)%3], Strings: styles[int(seed)%len(styles)]}
			src := "obj={v=1}\narr=[{v=1}]\n"
			if format == "yaml" {
				src = "obj: {v: 1}\narr:\n  - v: 1\n"
			}
			if seed%4 == 1 {
				src = "[obj]\nv=1\n[[arr]]\nv=1\n"
				if format == "yaml" {
					src = "obj:\n    v: 1\narr: [{v: 1}]\n"
				}
			}
			if seed%4 == 2 {
				src = ""
			}
			if seed%4 == 3 {
				src = "obj={\n    v=1,\n}\narr=[1,2]\n"
				if format == "yaml" {
					src = "obj: {\n    v: 1,\n}\narr: [1,2]\n"
				}
			}
			policy.Keys = []string{"auto", "plain", "single", "double"}[int(seed/4)%4]
			policy.Indent = []string{"", " ", "    ", "          "}[int(seed/8)%4]
			if seed%2 == 1 {
				policy.LineEnding = "crlf"
			}

			d, e := tql.Parse([]byte(src), format, tql.WithStyle(policy))
			if e != nil {
				t.Fatal(e)
			}
			value := map[string]any{"value": v, "nested": map[string]any{"value": v}, "list": []any{v, map[string]any{"value": v}}}
			path := edit.Key("added")
			if seed%4 == 0 {
				path = edit.Key("obj", "added")
			}
			if seed%4 == 1 {
				path = edit.Key("arr").Index(1)
			}
			if _, e = d.Apply(edit.Set(path, value)); e != nil {
				t.Fatalf("%s style=%+v value=%q: %v", format, policy, v, e)
			}
		}
	})
}

func TestStyleDoesNotLeakBetweenDocuments(t *testing.T) {
	source := []byte("old={v=1}\n")
	registry := tql.NewRegistry()
	if err := registry.Register("custom", tomlformat.Format{}); err != nil {
		t.Fatal(err)
	}
	styled, err := registry.Parse(source, "custom", tql.WithStyle(tql.Style{Objects: "block"}))
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := registry.Parse(source, "custom")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = styled.Exec("new.v=2"); err != nil {
		t.Fatal(err)
	}
	if _, err = automatic.Exec("new.v=2"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(styled.Bytes()), "[new]") || strings.Contains(string(automatic.Bytes()), "[new]") {
		t.Fatal("registry policy leaked")
	}
	backend, err := (tomlformat.Format{}).WithStyle(tql.Style{Objects: "block"})
	if err != nil {
		t.Fatal(err)
	}
	reset, err := tql.ParseWith(source, backend, tql.WithStyle(tql.Style{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reset.Exec("new.v=2"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(reset.Bytes()), "[new]") {
		t.Fatal("explicit auto did not reset the backend policy")
	}
}
