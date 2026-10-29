package tql_test

import (
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestTOMLDeleteLayout(t *testing.T) {
	for _, tc := range []struct{ name, source, script, want string }{
		{"assignment", "# top\n  a = 1\n\n\nkeep='x' # keep\n", `del(a)`, "# top\n\n\nkeep='x' # keep\n"},
		{"multiline", "a = [\n  1,\n  2,\n]\nkeep=3\n", `del(a)`, "keep=3\n"},
		{"end", "keep=3\n  a=1", `del(a)`, "keep=3\n"},
		{"comment", "keep=3\na=1 # reason\n# next\nb=2\n", `del(a)`, "keep=3\n # reason\n# next\nb=2\n"},
		{"array tables", "# top\n\n[[workers]]\nname='a'\ncommand='a'\n\n[[workers]]\nname='b'\ncommand='b'\n\n[[workers]]\nname='c'\n\n\n[keep]\nx=1\n", `del(workers[0:2])`, "# top\n\n[[workers]]\nname='c'\n\n\n[keep]\nx=1\n"},
		{"nested tables", "[[workers]]\nname='a'\n[workers.settings]\nx=1\n\n# keep comment\n\n[[workers]]\nname='b'\n", `del(workers[0])`, "# keep comment\n\n[[workers]]\nname='b'\n"},
		{"all items", "[[workers]]\nname='a'\n\n[[workers]]\nname='b'\n\n[keep]\nx=1\n", `del(workers[])`, "workers = []\n\n[keep]\nx=1\n"},
		{"whole array", "[[workers]]\nname='a'\n\n[[workers]]\nname='b'\n\n[keep]\nx=1\n", `del(workers)`, "[keep]\nx=1\n"},
		{"array line", "x=[\n  1,\n  2,\n  3,\n]\n", `del(x[1])`, "x=[\n  1,\n  3,\n]\n"},
		{"array last line", "x=[\n  1,\n  2\n]\n", `del(x[1])`, "x=[\n  1\n]\n"},
		{"array comma line after comment", "x=[\n  1 # keep\n  ,\n  2\n]\n", `del(x[0])`, "x=[\n   # keep\n  2\n]\n"},
		{"array previous comma line", "x=[\n  1 # keep\n  ,\n  2\n]\n", `del(x[1])`, "x=[\n  1 # keep\n]\n"},
		{"array inline", "x=[ 1, 2, 3 ]\n", `del(x[1])`, "x=[ 1, 3 ]\n"},
		{"array empty padded", "x=[ 1, 2 ]\n", `del(x[])`, "x=[ ]\n"},
		{"array empty multiline", "x=[\n  1,\n  2,\n]\n", `del(x[])`, "x=[\n]\n"},
		{"array trailing comment", "x=[\n  1, # first\n  2, # second\n  3,\n]\n", `del(x[1])`, "x=[\n  1, # first\n   # second\n  3,\n]\n"},
		{"inline first", "x={ a=1, b=2, c=3 }\n", `del(x.a)`, "x={ b=2, c=3 }\n"},
		{"inline last", "x={ a=1, b=2, c=3 }\n", `del(x.c)`, "x={ a=1, b=2 }\n"},
		{"inline multiple", "x={ a=1, b=2, c=3, d=4 }\n", `del(x.b,x.c)`, "x={ a=1, d=4 }\n"},
		{"inline empty padded", "x={ a=1, b=2 }\n", `del(x[])`, "x={ }\n"},
		{"multiline inline", "x={\n  a=1,\n  b=2,\n  c=3,\n}\n", `del(x.b)`, "x={\n  a=1,\n  c=3,\n}\n"},
		{"dotted inline comment", "x={\n  a.b=1, # keep\n  a.c=2,\n  z=3,\n}\n", `del(x.a)`, "x={\n   # keep\n  z=3,\n}\n"},
		{"dotted inline comment last", "x={\n  z=3,\n  a.b=1, # keep\n  a.c=2\n}\n", `del(x.a)`, "x={\n  z=3\n   # keep\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, nl := range []string{"\n", "\r\n"} {
				source := strings.ReplaceAll(tc.source, "\n", nl)
				want := strings.ReplaceAll(tc.want, "\n", nl)
				doc, err := tql.Parse([]byte(source), "toml")
				if err != nil {
					t.Fatal(err)
				}
				result, err := doc.Exec(tc.script)
				if err != nil {
					t.Fatal(err)
				}
				if string(result.Bytes) != want {
					t.Fatalf("got %q\nwant %q", result.Bytes, want)
				}
			}
		})
	}
}

func TestTOMLArrayCommentEditContract(t *testing.T) {
	const source = "commands = [\n    'run a',\n#    'disabled command',\n]\nkeep = 'original' # untouched\n"
	t.Run("whole replacement", func(t *testing.T) {
		doc := document(t, source, "toml")
		result, err := doc.Exec(`commands = ["run x", "run b"]`)
		if err != nil {
			t.Fatal(err)
		}
		if string(result.Bytes) != "commands = [\n    'run x',\n    'run b',\n]\nkeep = 'original' # untouched\n" {
			t.Fatal(string(result.Bytes))
		}
	})
	t.Run("append preserves existing array", func(t *testing.T) {
		doc := document(t, source, "toml")
		result, err := doc.Exec(`let $index = length(commands); commands[$index] = "run b"`)
		if err != nil {
			t.Fatal(err)
		}
		want := "commands = [\n    'run a',\n#    'disabled command',\n    'run b',\n]\nkeep = 'original' # untouched\n"
		if string(result.Bytes) != want {
			t.Fatalf("got %q want %q", result.Bytes, want)
		}
		assertQuery(t, doc, "commands", []any{"run a", "run b"})
	})
}
