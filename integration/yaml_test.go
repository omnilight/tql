package integration_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestYAMLParserRegressionsThroughCLI(t *testing.T) {
	for _, tc := range []struct{ name, source, query, want string }{
		{"unicode root", "\u00a0\n", "$", "\"\u00a0\"\n"},
		{"unicode continuation", "v: foo\n  \u00a0\n  bar\n", "v", "\"foo \u00a0 bar\"\n"},
		{"folded tab", "v: >\n  \tfoo\n  bar\n", "v", "\"\\tfoo\\nbar\\n\"\n"},
		{"signed bases", "a: -0x10\nb: +0o10\nc: 0x10\nd: -10\n", "[a,b,c,d]", "[\"-0x10\",\"+0o10\",16,-10]\n"},
		{"inline plain marker", "--- foo\n", "$", "\"foo\"\n"},
		{"inline array marker", "--- [1,2]\n", "$", "[1,2]\n"},
		{"inline map marker", "--- {a: 1}\n", "$", "{\"a\":1}\n"},
		{"flow continuation", "a: [one\r\n  two]\r\n", "a", "[\"one two\"]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantResult(t, run(t, t.TempDir(), tc.source, "-f", "yaml", tc.query, "-"), 0, tc.want)
		})
	}
}

func TestYAMLFoldedTabEditThroughCLI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	source := "v: >\n  \tfoo\n  bar\nkeep: 0x10 # bytes\n"
	write(t, path, []byte(source))
	wantResult(t, run(t, dir, "", "-i", `v |= .`, path), 0, "")
	if got := read(t, path); got != source {
		t.Fatalf("no-op changed bytes: %q", got)
	}
	wantResult(t, run(t, dir, "", "-i", `v |= . + "!"`, path), 0, "")
	wantResult(t, run(t, dir, "", "v", path), 0, "\"\\tfoo\\nbar\\n!\"\n")
	after := read(t, path)
	if !strings.HasSuffix(after, "keep: 0x10 # bytes\n") {
		t.Fatalf("neighbor changed: %q", after)
	}
	wantResult(t, run(t, dir, "", "-i", `v |= .`, path), 0, "")
	if got := read(t, path); got != after {
		t.Fatal("repeated no-op changed bytes")
	}
	noTemps(t, dir)
}

func TestYAMLDepthBudgetThroughCLI(t *testing.T) {
	for _, tc := range []struct {
		blocks, flow int
		code         int
	}{{200, 56, 0}, {200, 57, 2}, {257, 0, 2}, {0, 300, 2}} {
		var b strings.Builder
		for i := range tc.blocks {
			b.WriteString(strings.Repeat("  ", i))
			b.WriteString("a:\n")
		}
		b.WriteString(strings.Repeat("  ", tc.blocks))
		b.WriteString(strings.Repeat("[", tc.flow))
		b.WriteString("0")
		b.WriteString(strings.Repeat("]", tc.flow))
		b.WriteByte('\n')
		want := ""
		if tc.code == 0 {
			want = "0\n"
		}
		query := strings.Repeat("a.", tc.blocks)
		query = strings.TrimSuffix(query, ".")
		if query == "" {
			query = "$"
		}
		query += strings.Repeat("[0]", tc.flow)
		wantResult(t, run(t, t.TempDir(), b.String(), "-f", "yaml", query, "-"), tc.code, want)
	}
}

func TestYAMLInlineMarkerScalarThroughCLI(t *testing.T) {
	for _, source := range []string{"--- ...\n", "--- ... # keep\n", "--- --- # keep\r\n"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		value := "..."
		if strings.HasPrefix(source, "--- ---") {
			value = "---"
		}
		write(t, path, []byte(source))
		wantResult(t, run(t, dir, "", ".", path), 0, "\""+value+"\"\n")
		for range 2 {
			wantResult(t, run(t, dir, "", "-i", `. = "`+value+`"`, path), 0, "")
			if got := read(t, path); got != source {
				t.Fatalf("same scalar changed bytes: %q", got)
			}
		}
		noTemps(t, dir)
	}
}
