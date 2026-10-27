package tql_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestYAMLScalarRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		name, source, query string
		want                any
	}{
		{"unicode root", "\u00a0\n", "$", "\u00a0"},
		{"unicode continuation", "v: foo\n  \u00a0\n  bar\nkeep: 01 # same\n", "v", "foo \u00a0 bar"},
		{"folded tab", "v: >\n  \tfoo\n  bar\nkeep: 01 # same\n", "v", "\tfoo\nbar\n"},
		{"signed hex", "v: -0x10\nkeep: 01 # same\n", "v", "-0x10"},
		{"signed octal", "v: +0o10\nkeep: 01 # same\n", "v", "+0o10"},
		{"unsigned hex", "v: 0x10\n", "v", tql.Number("16")},
		{"signed decimal", "v: -10\n", "v", tql.Number("-10")},
		{"inline marker", "--- foo # same\r\n", "$", "foo"},
		{"flow continuation", "v: [one\r\n  two]\r\nkeep: 01 # same\r\n", "v[0]", "one two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := document(t, tc.source, "yaml")
			values, err := d.Query(tc.query)
			if err != nil || !reflect.DeepEqual(values, []any{tc.want}) {
				t.Fatalf("read: %#v, %v; want %#v", values, err, tc.want)
			}
			for range 2 {
				r, err := d.Exec(tc.query + " |= .")
				if err != nil {
					t.Fatal(err)
				}
				if r.Changed || !bytes.Equal(r.Bytes, []byte(tc.source)) {
					t.Fatalf("no-op changed bytes: %q", r.Bytes)
				}
			}
			if value, ok := tc.want.(string); ok {
				r, err := d.Exec(tc.query + ` |= . + "!"`)
				if err != nil {
					t.Fatal(err)
				}
				values, err = d.Query(tc.query)
				if err != nil || !reflect.DeepEqual(values, []any{value + "!"}) {
					t.Fatalf("edited value: %#v, %v", values, err)
				}
				if bytes.Contains([]byte(tc.source), []byte("keep: 01 # same")) && !bytes.Contains(r.Bytes, []byte("keep: 01 # same")) {
					t.Fatalf("neighbor changed: %q", r.Bytes)
				}
				before := d.Bytes()
				if r, err := d.Exec(tc.query + ` = "temporary"; 1 / 0`); err == nil || r != nil {
					t.Fatal("expected rollback error without result")
				}
				if !bytes.Equal(before, d.Bytes()) {
					t.Fatal("failed transaction changed bytes")
				}
			}
		})
	}
}
