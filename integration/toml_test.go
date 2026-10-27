package integration_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTOML11SyntaxThroughCLI(t *testing.T) {
	for _, tc := range []struct{ name, source, query, want string }{
		{"new escapes", "value = \"\\x41\\e\\xFF\"\n", "value", "\"A\\u001bÿ\"\n"},
		{"optional seconds", "local = 07:32\noffset = 1979-05-27T07:32Z\n", "[local, offset]", "[\"07:32\",\"1979-05-27T07:32Z\"]\n"},
		{"multiline inline table", "settings = {\n a = 1, # keep\n b = true,\n}\n", "settings", "{\"a\":1,\"b\":true}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			wantResult(t, run(t, dir, tc.source, "-f", "toml", tc.query, "-"), 0, tc.want)
			wantResult(t, run(t, dir, tc.source, "-f", "toml", ". = .", "-"), 0, tc.source)
		})
	}
}

func TestTOML11EditsThroughCLI(t *testing.T) {
	for _, nl := range []string{"\n", "\r\n"} {
		t.Run(nl, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			source := strings.ReplaceAll("settings = {\n  a = 1, # keep\n}\nclock = 07:32\nneighbor = 0xFf # stable\n", "\n", nl)
			write(t, path, []byte(source))
			wantResult(t, run(t, dir, "", "-i", `settings.a = 2`, path), 0, "")
			want := strings.Replace(source, "a = 1", "a = 2", 1)
			if got := read(t, path); got != want {
				t.Fatalf("neighboring bytes changed: got %q want %q", got, want)
			}
			for range 2 {
				wantResult(t, run(t, dir, "", "-i", `settings.a = 2`, path), 0, "")
				if got := read(t, path); got != want {
					t.Fatalf("no-op changed bytes: %q", got)
				}
			}
			wantResult(t, run(t, dir, "", "-i", `settings.a = 3; 1 / 0`, path), 2, "")
			if got := read(t, path); got != want {
				t.Fatalf("failed transaction changed bytes: %q", got)
			}
			noTemps(t, dir)
		})
	}
}
