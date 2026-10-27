package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLIParameters(t *testing.T) {
	for _, tt := range []struct {
		name         string
		flags        []string
		script, want string
		code         int
	}{
		{"string", []string{"--arg", "p=001"}, `param $p; $p`, "\"001\"\n", 0},
		{"equals", []string{"--arg=p=a=b"}, `param $p; $p`, "\"a=b\"\n", 0},
		{"empty string", []string{"--arg", "p="}, `param $p = "default"; $p`, "\"\"\n", 0},
		{"unicode", []string{"--arg", "имя=alice"}, `param $имя; $имя`, "\"alice\"\n", 0},
		{"exact number", []string{"--argjson", "p=9007199254740993"}, `param $p; $p + 1`, "9007199254740994\n", 0},
		{"object", []string{"--argjson", `p={"x":[true,null,1]}`}, `param $p; $p`, "{\"x\":[true,null,1]}\n", 0},
		{"null", []string{"--argjson", "p=null"}, `param $p = 3; $p`, "null\n", 0},
		{"raw", []string{"-r", "--arg", "p=hello"}, `param $p; $p`, "hello\n", 0},
		{"values", []string{"--values", "--argjson", "p=3"}, `param $p; n = $p; n`, "3\n", 0},
		{"check changed", []string{"--check", "--argjson", "p=3"}, `param $p; n = $p`, "", 1},
		{"check unchanged", []string{"--check", "--argjson", "p=1"}, `param $p; n = $p`, "", 0},
		{"duplicate", []string{"--arg", "p=x", "--argjson", "p=1"}, `param $p; $p`, "", 2},
		{"unknown", []string{"--arg", "typo=x"}, `param $p = null; $p`, "", 2},
		{"missing", nil, `param $p; n = $p`, "", 2},
		{"invalid name", []string{"--arg", "$p=x"}, `1`, "", 2},
		{"no equals", []string{"--arg", "p"}, `1`, "", 2},
		{"invalid JSON", []string{"--argjson", "p=hello"}, `param $p; $p`, "", 2},
		{"trailing JSON", []string{"--argjson", "p=1 2"}, `param $p; $p`, "", 2},
		{"JSON duplicate", []string{"--argjson", `p={"x":1,"x":2}`}, `param $p; $p`, "", 2},
		{"JSON is data", []string{"--argjson", "p=1 + 2"}, `param $p; $p`, "", 2},
		{"reserved stdin", []string{"--params-file", "-"}, `1`, "", 2},
		{"missing file", []string{"--params-file", filepath.Join(t.TempDir(), "missing.json")}, `1`, "", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"-f", "json"}, tt.flags...)
			args = append(args, tt.script)
			code, out, diagnostic := invoke(args, `{"n":1}`)
			if code != tt.code || out != tt.want {
				t.Fatalf("code=%d out=%q err=%s; want %d %q", code, out, diagnostic, tt.code, tt.want)
			}
			if code == 2 && diagnostic == "" {
				t.Fatal("missing diagnostic")
			}
		})
	}
}

func TestCLIParametersFile(t *testing.T) {
	for _, tt := range []struct {
		content string
		flags   []string
		want    string
		code    int
	}{
		{`{"p":9007199254740993}`, nil, "9007199254740993\n", 0},
		{`{}`, nil, "2\n", 0},
		{`{"p":null}`, nil, "null\n", 0},
		{`{"p":1}`, []string{"--argjson", "p=3"}, "3\n", 0},
		{`{"p":1}`, []string{"--arg", "p=001"}, "\"001\"\n", 0},
		{`{"p":1,"p":2}`, nil, "", 2},
		{`{"p":}`, nil, "", 2},
		{`[]`, nil, "", 2},
		{`null`, nil, "", 2},
		{`{"typo":1}`, nil, "", 2},
		{`{"$p":1}`, nil, "", 2},
	} {
		path := filepath.Join(t.TempDir(), "params.json")
		if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
			t.Fatal(err)
		}
		// Exercise both orders: explicit flags always override the file.
		for _, flagsFirst := range []bool{true, false} {
			args := []string{"-f", "json"}
			if flagsFirst {
				args = append(args, tt.flags...)
			}
			args = append(args, "--params-file", path)
			if !flagsFirst {
				args = append(args, tt.flags...)
			}
			args = append(args, `param $p = 2; $p`)
			code, out, diagnostic := invoke(args, `{}`)
			if code != tt.code || out != tt.want || (code == 2 && diagnostic == "") {
				t.Fatalf("%v: code=%d out=%q err=%s", args, code, out, diagnostic)
			}
		}
		if code, _, _ := invoke([]string{"--params-file", path, "--params-file", path, "1"}, ""); code != 2 {
			t.Fatal("accepted multiple files")
		}
	}
}
