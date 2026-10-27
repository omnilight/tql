// Package integration_test exercises the installed command boundary: a compiled
// binary, real source/script files, stdin/stdout, exit codes and filesystem state.
package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tql-integration-binary-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "tql")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "../cmd/tql")
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build tql: %v\n%s", err, output)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

type result struct {
	code   int
	stdout string
	stderr string
}

func run(t *testing.T, dir, input string, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = dir
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else {
			t.Fatalf("execute tql: %v", err)
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("tql timed out: %v; stderr: %s", args, stderr.String())
	}
	return result{code, stdout.String(), stderr.String()}
}

func wantResult(t *testing.T, got result, code int, stdout string) {
	t.Helper()
	if strings.Contains(got.stderr, "panic:") || strings.Contains(got.stderr, "runtime error:") {
		t.Fatalf("command panicked instead of returning a diagnostic: %s", got.stderr)
	}
	if got.code != code || got.stdout != stdout {
		t.Fatalf("exit=%d, stdout=%q, stderr=%q; want exit=%d, stdout=%q", got.code, got.stdout, got.stderr, code, stdout)
	}
	if code == 2 {
		if got.stderr == "" {
			t.Fatal("error exit has no diagnostic")
		}
	} else if got.stderr != "" {
		t.Fatalf("unexpected stderr: %s", got.stderr)
	}
}

func fixture(t *testing.T, dir, format string) (string, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "config."+format))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config."+format)
	write(t, path, data)
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	return path, string(data)
}

func script(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "scripts", name+".tql"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".tql")
	write(t, path, data)
	return path
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func stat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func noTemps(t *testing.T, dir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, ".tql-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("transaction left temporary files: %v", files)
	}
}

func expectPorts(source, format, first, second string) string {
	replacements := []string{first, second}
	// Replace the later value first: walk's 80 -> 81 must not become the
	// subsequent replacement's original 81 target.
	for index, replacement := range slices.Backward(replacements) {
		old := fmt.Sprint(80 + index)
		switch format {
		case "json":
			// Both JSON spacing variants are intentional test input.
			if index == 0 {
				source = strings.Replace(source, `"port" : `+old, `"port" : `+replacement, 1)
			} else {
				source = strings.Replace(source, `"port": `+old, `"port": `+replacement, 1)
			}
		case "yaml":
			source = strings.Replace(source, "port: "+old, "port: "+replacement, 1)
		case "toml":
			source = strings.Replace(source, "port = "+old, "port = "+replacement, 1)
		}
	}
	return source
}

func TestQueryFileEditsExactSourceBytes(t *testing.T) {
	for _, format := range []string{"json", "toml", "yaml"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			path, source := fixture(t, dir, format)
			mode := stat(t, path).Mode().Perm()
			program := script(t, dir, "update")
			wantResult(t, run(t, dir, "", "--query-file", program, "-i", path), 0, "")
			want := expectPorts(source, format, "90", "0")
			if got := read(t, path); got != want {
				t.Fatalf("source formatting changed beyond requested values:\ngot  %q\nwant %q", got, want)
			}
			if got := stat(t, path).Mode().Perm(); got != mode {
				t.Fatalf("file permissions changed: got %v, want %v", got, mode)
			}
			noTemps(t, dir)
		})
	}
}

func TestRecursiveWalkPreservesFormat(t *testing.T) {
	for _, format := range []string{"json", "toml", "yaml"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			path, source := fixture(t, dir, format)
			program := script(t, dir, "walk")
			wantResult(t, run(t, dir, "", "-i", "--query-file", program, path), 0, "")
			want := expectPorts(source, format, "81", "82")
			if got := read(t, path); got != want {
				t.Fatalf("walk changed wrong bytes:\ngot  %q\nwant %q", got, want)
			}
			noTemps(t, dir)
		})
	}
}

func TestReadsAndNoOpPreserveFileIdentity(t *testing.T) {
	for _, format := range []string{"json", "toml", "yaml"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			path, source := fixture(t, dir, format)
			// A fixed old timestamp makes unintended rewrites detectable even on
			// filesystems whose timestamp resolution is coarser than this test.
			stamp := time.Unix(1_600_000_000, 0)
			if err := os.Chtimes(path, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			before := stat(t, path)
			wantResult(t, run(t, dir, "", "title", path), 0, "\"Сервис 🌳\"\n")
			wantResult(t, run(t, dir, "", "-i", `for $server in servers[] { $server.port |= .; }`, path), 0, "")
			after := stat(t, path)
			if !before.ModTime().Equal(after.ModTime()) || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatalf("read/no-op replaced or touched file: before=%v after=%v", before, after)
			}
			if got := read(t, path); got != source {
				t.Fatalf("read/no-op changed input bytes: %q", got)
			}
			noTemps(t, dir)
		})
	}
}

func TestLateLoopFailureRollsBackFile(t *testing.T) {
	for _, format := range []string{"json", "toml", "yaml"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			path, source := fixture(t, dir, format)
			program := script(t, dir, "rollback")
			before := stat(t, path)
			got := run(t, dir, "", "-i", "--query-file", program, path)
			wantResult(t, got, 2, "")
			if !strings.Contains(strings.ToLower(got.stderr), "zero") {
				t.Fatalf("expected execution to reach late division by zero: %s", got.stderr)
			}
			after := stat(t, path)
			if read(t, path) != source || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("failed loop changed the original file")
			}
			noTemps(t, dir)
		})
	}
}

func TestCheckReportsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	path, source := fixture(t, dir, "yaml")
	program := script(t, dir, "update")
	before := stat(t, path)
	wantResult(t, run(t, dir, "", "--check", "--query-file", program, path), 1, "")
	wantResult(t, run(t, dir, "", "--check", `for $s in servers[] { $s.port |= .; }`, path), 0, "")
	wantResult(t, run(t, dir, "", "--check", `let $x = 1 / 0; servers[0].port = $x`, path), 2, "")
	after := stat(t, path)
	if read(t, path) != source || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("--check wrote input file")
	}
	noTemps(t, dir)
}

func TestStdinMutationAndValuesHaveNoFilesystemSideEffects(t *testing.T) {
	dir := t.TempDir()
	path, source := fixture(t, dir, "json")
	program := script(t, dir, "update")
	wantResult(t, run(t, dir, source, "--format", "json", "--query-file", program, "-"), 0, expectPorts(source, "json", "90", "0"))
	query := `let $delta = 2; for $s in servers[] { $s.port += $delta; $s.port; }; title`
	wantResult(t, run(t, dir, source, "-f", "json", "--values", query), 0, "82\n83\n\"Сервис 🌳\"\n")
	query = `servers[0].port; for $s in servers[] { $s.port = 1 / 0; }`
	wantResult(t, run(t, dir, source, "-f", "json", "--values", query), 2, "")
	if got := read(t, path); got != source {
		t.Fatalf("stdin invocation changed unrelated file: %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("stdin invocation created filesystem entries: %v", entries)
	}
}

func TestLoopDeletionUsesOriginalNodeReferences(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")
	write(t, path, []byte(`{"items": [1, 2, 3, 4, 5], "keep": "untouched"}`))
	query := `for $item in items[] { if $item == 2 or $item == 4 { del($item); } else { $item += 10; } }`
	wantResult(t, run(t, dir, "", "-i", query, path), 0, "")
	wantResult(t, run(t, dir, "", "items", path), 0, "[11,13,15]\n")
	if !strings.Contains(read(t, path), `"keep": "untouched"`) {
		t.Fatal("deleting loop items changed unrelated field")
	}
	noTemps(t, dir)
}

func TestWhileBreakContinueAndDynamicIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")
	source := "{\n  \"items\": [0, 0, 0, 0, 0],\n  \"labels\": {\"name\": \"before\"}\n}\n"
	write(t, path, []byte(source))
	query := `let $i = 0; while $i < 5 { $i += 1; if $i == 2 { continue; } if $i == 5 { break; } items[$i - 1] = $i; }; let $key = "name"; labels[$key] = "after"`
	wantResult(t, run(t, dir, "", "-i", query, path), 0, "")
	want := strings.Replace(source, "[0, 0, 0, 0, 0]", "[1, 0, 3, 4, 0]", 1)
	want = strings.Replace(want, `"before"`, `"after"`, 1)
	if got := read(t, path); got != want {
		t.Fatalf("dynamic edits changed wrong bytes:\ngot  %q\nwant %q", got, want)
	}
}

func TestBlockVariableDoesNotEscapeAndRollsBack(t *testing.T) {
	dir := t.TempDir()
	path, source := fixture(t, dir, "json")
	query := `servers[0].port = 100; if true { let $hidden = 7; }; servers[1].port = $hidden`
	wantResult(t, run(t, dir, "", "-i", query, path), 2, "")
	if got := read(t, path); got != source {
		t.Fatalf("scope error did not roll back: %q", got)
	}
	noTemps(t, dir)
}

func TestExistingQueriesStillWorkThroughBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "original.json")
	write(t, path, []byte(`{"foo":{"bar":0},"baz":[{}, {"name":0}],"obj":[],"objs":[{"name":"foo"},{"name":"other"}]}`))
	query := `foo.bar = 1; baz[1].name = 3; obj = [1,2,3]; objs[name == "foo"] = "abc"; del(foo.bar)`
	wantResult(t, run(t, dir, "", "-i", query, path), 0, "")
	wantResult(t, run(t, dir, "", `[foo, baz[1].name, obj, objs]`, path), 0, `[{},3,[1,2,3],["abc",{"name":"other"}]]`+"\n")
	noTemps(t, dir)
}
