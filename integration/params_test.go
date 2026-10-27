package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParameterizedScriptFiles(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "rollout.tql")
	params := filepath.Join(dir, "params.json")
	input := filepath.Join(dir, "config.toml")
	write(t, program, []byte(`param $port; param $enabled = true; server.port = $port; server.enabled = $enabled; server.port`))
	write(t, params, []byte(`{"port":9090}`))
	source := "# keep\r\n[server]\r\nport  = 8080 # public\r\nenabled = false\r\n"
	want := "# keep\r\n[server]\r\nport  = 9090 # public\r\nenabled = true\r\n"
	write(t, input, []byte(source))
	args := []string{"--params-file", params, "--query-file", program, input}
	wantResult(t, run(t, dir, "", args...), 0, want)
	if read(t, input) != source {
		t.Fatal("preview wrote input")
	}
	wantResult(t, run(t, dir, "", append([]string{"--check"}, args...)...), 1, "")
	wantResult(t, run(t, dir, "", append([]string{"--values"}, args...)...), 0, "9090\n")
	wantResult(t, run(t, dir, "", append([]string{"-i"}, args...)...), 0, "")
	if read(t, input) != want {
		t.Fatal("in-place output differs")
	}
	before, err := os.Stat(input)
	if err != nil {
		t.Fatal(err)
	}
	wantResult(t, run(t, dir, "", append([]string{"-i"}, args...)...), 0, "")
	after, err := os.Stat(input)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("no-op replaced input")
	}
	for _, content := range []string{`{}`, `{"typo":1}`, `{"port":null}`} {
		write(t, params, []byte(content))
		wantResult(t, run(t, dir, "", append([]string{"-i"}, args...)...), 2, "")
		if read(t, input) != want {
			t.Fatal("parameter failure changed input")
		}
	}
	write(t, program, []byte(`param $port; server.port = $port; server.port; $port += 1`))
	write(t, params, []byte(`{"port":1234}`))
	wantResult(t, run(t, dir, "", append([]string{"-i"}, args...)...), 2, "")
	if read(t, input) != want {
		t.Fatal("read-only parameter failure committed input")
	}
	wantResult(t, run(t, dir, `{"n":0}`, "-f", "json", "--argjson", "n=9007199254740993", `param $n; n = $n`), 0, `{"n":9007199254740993}`)
}

func TestParameterExample(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"compose.yaml", "rollout.tql", "production.json", "expected.yaml"} {
		data, err := os.ReadFile(filepath.Join("..", "examples", "parameters", name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, name), data)
	}
	want := read(t, filepath.Join(dir, "expected.yaml"))
	for _, flags := range [][]string{
		{"--params-file", "production.json"},
		{"--arg", "service=api", "--arg", "image=api:v2", "--argjson", "replicas=3"},
	} {
		args := append(flags, "--query-file", "rollout.tql", "compose.yaml")
		wantResult(t, run(t, dir, "", args...), 0, want)
		wantResult(t, run(t, dir, "", append([]string{"-i"}, args...)...), 0, "")
		wantResult(t, run(t, dir, "", append([]string{"--check"}, args...)...), 0, "")
	}
}
