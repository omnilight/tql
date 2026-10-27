package tql

import (
	"strings"
	"testing"
)

func TestYAMLBlockTemplateTagPreservesSource(t *testing.T) {
	source := "# keep\nport: 80 # public\nadd_tags:\n  - pod:{{ .pod }} # template\n"
	doc, err := Parse([]byte(source), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	result, err := doc.Exec(`add_tags[0] = "pod:{{ .pod }}"`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || string(result.Bytes) != source {
		t.Fatalf("no-op changed source: %q", result.Bytes)
	}
	result, err = doc.Exec("port = 81")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(result.Bytes), strings.Replace(source, "port: 80", "port: 81", 1); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
