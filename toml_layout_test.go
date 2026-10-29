package tql_test

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vasileknik76/tql"
)

func TestTOMLInsertThenDeleteSection(t *testing.T) {
	const source = "[[tiles]]\nlabel='circle'\nunused=9\n\n[[tiles]]\nlabel='square'\nunused=8\n\n[board]\nwidth=4\n"
	const want = "[[tiles]]\nlabel='circle'\ncolors=['red', 'blue']\n\n[board]\nwidth=4\n"
	for _, script := range []string{
		`tiles[0].colors=["red","blue"]; del(tiles[0].unused); del(tiles[1])`,
		`del(tiles[0].unused); del(tiles[1]); tiles[0].colors=["red","blue"]`,
	} {
		for _, nl := range []string{"\n", "\r\n"} {
			doc := document(t, strings.ReplaceAll(source, "\n", nl), "toml")
			result, err := doc.Exec(script)
			if err != nil {
				t.Fatal(err)
			}
			if expected := strings.ReplaceAll(want, "\n", nl); string(result.Bytes) != expected {
				t.Fatalf("got %q\nwant %q", result.Bytes, expected)
			}
			assertQuery(t, doc, "tiles[0].colors", []any{"red", "blue"})
		}
	}
	doc := document(t, source, "toml")
	if _, err := doc.Exec(`tiles[0].colors=["red","blue"]; del(tiles[1]); board.width=null`); err == nil {
		t.Fatal("expected unrepresentable TOML value")
	}
	if string(doc.Bytes()) != source {
		t.Fatal("failed transaction changed source")
	}
}

func TestTOMLArrayPrefixRollback(t *testing.T) {
	const source = "colors=[\n  'red', # warm\n]\nshape='circle'\n"
	doc := document(t, source, "toml")
	if _, err := doc.Exec(`param $items; colors=$items`, tql.WithParams(map[string]any{"items": []any{"red", "blue", nil}})); err == nil {
		t.Fatal("expected unrepresentable TOML value")
	}
	if string(doc.Bytes()) != source {
		t.Fatal("failed array extension changed source")
	}
}

func FuzzTOMLArrayReplacement(f *testing.F) {
	for i, value := range []string{"blue", "can't", "a\nb", "\r\n", "# note", "\x00", "'''", `"""`} {
		f.Add(uint8(i), value)
	}
	program, err := tql.Compile(`param $items; colors=$items`)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, mode uint8, value string) {
		if len(value) > 256 || !utf8.ValidString(value) {
			t.Skip()
		}
		sources := []string{"colors=['red',]\n", "colors=[\n  'red', # warm\n]\n", "colors=[\n  # Choose a shade.\n]\n", "colors=[ 'green' , 'gold' , ]\n"}
		nl := "\n"
		if mode%2 == 1 {
			nl = "\r\n"
		}
		const suffix = "shape='circle' # boundary\n"
		source := strings.ReplaceAll(sources[int(mode)%len(sources)]+suffix, "\n", nl)
		doc := document(t, source, "toml")
		items := []any{"red", value}
		result, err := doc.Execute(program, tql.WithParams(map[string]any{"items": items}))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(doc.Value().(map[string]any)["colors"], items) {
			t.Fatal("array values changed")
		}
		if !strings.HasSuffix(string(result.Bytes), strings.ReplaceAll(suffix, "\n", nl)) {
			t.Fatal("unrelated source changed")
		}
	})
}

func TestTOMLArrayReplacementLayout(t *testing.T) {
	for _, tc := range []struct{ name, source, script, want string }{
		{"own multiline template", "colors = [\n    'red',\n    'blue',\n]\nshape='circle' # untouched\n", `colors=["green","gold"]`, "colors = [\n    'green',\n    'gold',\n]\nshape='circle' # untouched\n"},
		{"own compact template", "colors=['red','blue']\n", `colors=["green","gold"]`, "colors=['green','gold']\n"},
		{"own spaced template", "colors = [ 'red' , 'blue' , ]\n", `colors=["green","gold"]`, "colors = [ 'green' , 'gold' , ]\n"},
		{"no trailing comma", "colors=[\n\t'red',\n\t'blue'\n]\n", `colors=["green","gold"]`, "colors=[\n\t'green',\n\t'gold'\n]\n"},
		{"indented assignment", "[palette]\n  colors=[\n      'red',\n      'blue',\n    ]\n", `palette.colors=["green","gold"]`, "[palette]\n  colors=[\n      'green',\n      'gold',\n    ]\n"},
		{"independent assignment indentation", "[palette]\n  colors=[\n      'red',\n      'blue',\n    ]\n\tother=1\n", `palette.colors=["green","gold"]`, "[palette]\n  colors=[\n      'green',\n      'gold',\n    ]\n\tother=1\n"},
		{"empty multiline replacement", "colors=[\n  'red',\n]\n", `colors=[]`, "colors=[\n]\n"},
		{"nested arrays", "grid = [\n  [1,2],\n  [3,4],\n]\n", `grid=[[5,6],[7,8]]`, "grid = [\n  [5,6],\n  [7,8],\n]\n"},
		{"array in inline table", "box={colors=['red','blue'],size=4}\n", `box.colors=["green","gold"]`, "box={colors=['green','gold'],size=4}\n"},
		{"prefix with note", "colors=[\n  'red', # warm\n  # Other shades are optional.\n]\nshape='circle'\n", `colors=["red","blue","gold"]`, "colors=[\n  'red', # warm\n  # Other shades are optional.\n  'blue',\n  'gold',\n]\nshape='circle'\n"},
		{"prefix compact", "colors=['red',]\n", `colors=["red","blue","gold"]`, "colors=['red','blue','gold',]\n"},
		{"prefix no trailing comma", "colors=[\n  'red' # warm\n]\n", `colors=["red","blue","gold"]`, "colors=[\n  'red', # warm\n  'blue',\n  'gold'\n]\n"},
		{"empty with note", "colors=[\n  # Choose a shade.\n]\n", `colors=["blue","gold"]`, "colors=[\n  # Choose a shade.\n  \"blue\",\n  \"gold\"\n]\n"},
		{"reordering has no guessed notes", "colors=[\n  'red', # warm\n  'blue',\n]\n", `colors=["blue","red"]`, "colors=[\n  'blue',\n  'red',\n]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, nl := range []string{"\n", "\r\n"} {
				doc := document(t, strings.ReplaceAll(tc.source, "\n", nl), "toml")
				result, err := doc.Exec(tc.script)
				if err != nil {
					t.Fatal(err)
				}
				if want := strings.ReplaceAll(tc.want, "\n", nl); string(result.Bytes) != want {
					t.Fatalf("got %q\nwant %q", result.Bytes, want)
				}
			}
		})
	}
}

func TestTOMLArrayReplacementPolicy(t *testing.T) {
	for _, tc := range []struct {
		style tql.Style
		want  string
	}{
		{tql.Style{Arrays: "flow"}, "colors=['red', 'blue']\n"},
		{tql.Style{Arrays: "block", Indent: "    ", Strings: "double"}, "colors=[\n    \"red\",\n    \"blue\"\n]\n"},
	} {
		doc, err := tql.Parse([]byte("colors=[\n  'red'\n]\n"), "toml", tql.WithStyle(tc.style))
		if err != nil {
			t.Fatal(err)
		}
		result, err := doc.Exec(`colors=["red","blue"]`)
		if err != nil {
			t.Fatal(err)
		}
		if string(result.Bytes) != tc.want {
			t.Fatalf("got %q want %q", result.Bytes, tc.want)
		}
	}
}
