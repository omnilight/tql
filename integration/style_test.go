package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInsertionsFollowFileStyle(t *testing.T) {
	cases := []struct {
		name, format, source, query, want string
	}{
		{
			name: "json tabs and CRLF", format: "json",
			source: "{\r\n\t\"title\" : \"before\",\r\n\t\"count\" : 1\r\n}",
			query:  `label = "after"`,
			want:   "{\r\n\t\"title\" : \"before\",\r\n\t\"count\" : 1,\r\n\t\"label\" : \"after\"\r\n}",
		},
		{
			name: "json compact array", format: "json",
			source: `{"names":[1,2],"keep":"\u0061"}`,
			query:  `names[2] = 3`,
			want:   `{"names":[1,2,3],"keep":"\u0061"}`,
		},
		{
			name: "toml quotes and CRLF", format: "toml",
			source: "title='before'\r\ncount=1 # keep\r\n",
			query:  `label = "after"`,
			want:   "title='before'\r\ncount=1 # keep\r\nlabel='after'\r\n",
		},
		{
			name: "toml missing final newline", format: "toml",
			source: "title='before'",
			query:  `label = "after"`,
			want:   "title='before'\nlabel='after'",
		},
		{
			name: "toml compact array", format: "toml",
			source: "names=[1,2] # keep\n",
			query:  `names[2] = 3`,
			want:   "names=[1,2,3] # keep\n",
		},
		{
			name: "yaml quotes and CRLF", format: "yaml",
			source: "'title': 'before'\r\n'count': 1 # keep\r\n",
			query:  `label = "after"`,
			want:   "'title': 'before'\r\n'count': 1 # keep\r\n'label': 'after'\r\n",
		},
		{
			name: "yaml missing final newline", format: "yaml",
			source: "'title': 'before'",
			query:  `label = "after"`,
			want:   "'title': 'before'\n'label': 'after'",
		},
		{
			name: "yaml block array", format: "yaml",
			source: "names:\n    - old\n    - keep # keep\n",
			query:  `names[2] = "new"`,
			want:   "names:\n    - old\n    - keep # keep\n    - new\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config."+tc.format)
			write(t, path, []byte(tc.source))
			wantResult(t, run(t, dir, "", "-i", tc.query, path), 0, "")
			if got := read(t, path); got != tc.want {
				t.Fatalf("inserted text did not follow local style:\ngot  %q\nwant %q", got, tc.want)
			}
			// A repeated assignment must leave both spelling and file identity intact.
			before := stat(t, path)
			wantResult(t, run(t, dir, "", "--check", tc.query, path), 0, "")
			if after := stat(t, path); !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || read(t, path) != tc.want {
				t.Fatal("checking the same insertion changed the file")
			}
			noTemps(t, dir)
		})
	}
}

func TestInsertedStringsRetainTypesAndEscapes(t *testing.T) {
	for _, tc := range []struct{ format, source string }{
		{"json", `{"label":"old"}`},
		{"toml", "label='old' # keep\n"},
		{"yaml", "label: old # keep\n"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config."+tc.format)
			write(t, path, []byte(tc.source))
			query := `.inserted = []; for $s in ["true", "12", "null", "a: b", "a # b", "can't", "line\nbreak", "é🌳"][] { let $i = length($.inserted); $.inserted[$i] = $s; }`
			wantResult(t, run(t, dir, "", "-i", query, path), 0, "")
			wantResult(t, run(t, dir, "", `.inserted`, path), 0, `["true","12","null","a: b","a # b","can't","line\nbreak","é🌳"]`+"\n")
			wantResult(t, run(t, dir, "", `label`, path), 0, "\"old\"\n")
			noTemps(t, dir)
		})
	}
}
