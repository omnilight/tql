package toml

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vasileknik76/tql/syntax"
)

func TestWideTablesPreserveOrderAndConflicts(t *testing.T) {
	for _, shape := range []string{"root", "header", "dotted", "inline", "array table"} {
		t.Run(shape, func(t *testing.T) {
			var b strings.Builder
			switch shape {
			case "header":
				b.WriteString("[wide]\n")
			case "inline":
				b.WriteString("wide={")
			case "array table":
				b.WriteString("[[wide]]\n")
			}
			for i := range 512 {
				if shape == "dotted" {
					b.WriteString("wide.")
				}
				fmt.Fprintf(&b, "k%d=%d", i, i)
				if shape == "inline" {
					if i < 511 {
						b.WriteByte(',')
					}
				} else {
					b.WriteByte('\n')
				}
			}
			if shape == "inline" {
				b.WriteString("}\n")
			}
			source := b.String()
			root := parse(t, source)
			table := root
			if shape != "root" {
				table = root.Get("wide")
			}
			if shape == "array table" {
				table = table.Items[0]
			}
			if len(table.Entries) != 512 {
				t.Fatalf("entries=%d", len(table.Entries))
			}
			for i, entry := range table.Entries {
				if entry.Key != fmt.Sprintf("k%d", i) || entry.Value.Value != syntax.Number(fmt.Sprint(i)) {
					t.Fatalf("entry %d: %#v", i, entry)
				}
			}
			edits, err := (Format{}).Replace([]byte(source), root, table.Get("k511"), syntax.Number("900"))
			got := apply(t, source, edits, err)
			if want := strings.Replace(source, "k511=511", "k511=900", 1); got != want {
				t.Fatalf("replacement changed neighboring bytes")
			}
			duplicate := source + "k511=900\n"
			switch shape {
			case "dotted":
				duplicate = source + "wide.k511=900\n"
			case "inline":
				duplicate = strings.TrimSuffix(source, "}\n") + ",k511=900}\n"
			}
			if n, err := (Format{}).Parse([]byte(duplicate)); err == nil || n != nil {
				t.Fatal("duplicate accepted or partial tree returned")
			}
		})
	}
}
