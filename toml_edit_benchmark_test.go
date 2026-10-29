package tql_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vasileknik76/tql"
)

// Synthetic public fixture: no production configuration or external corpus needed.
func workerEditSource() []byte {
	var src strings.Builder
	src.WriteString("title = 'example' # untouched\n\n")
	for i := range 40 {
		fmt.Fprintf(&src, "[[workers]]\nname = 'worker-%d'\ncommand = 'run %d'\nreplicas = 1\nsize = 'small'\n\n", i, i)
	}
	src.WriteString("[envs.test]\nflag = true # untouched\n")
	return []byte(src.String())
}

func BenchmarkTOMLWorkerEdits(b *testing.B) {
	for _, workload := range []struct{ name, script string }{
		{"Sequential", `for $w in workers[] { del($w.command); $w.name = "merged"; $w.commands = ["run a", "run b"]; $w.replicas = 2; $w.size = "large"; }`},
		{"DeleteSelection", `del(workers[replicas == 1])`},
		{"DeleteSubset", `del(workers[0:30])`},
		{"ReplaceSelection", `workers[].replicas = 2`},
	} {
		b.Run(workload.name, func(b *testing.B) {
			src := workerEditSource()
			p, err := tql.Compile(workload.script)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				doc, err := tql.Parse(src, "toml")
				if err != nil {
					b.Fatal(err)
				}
				if _, err := doc.Execute(p); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
