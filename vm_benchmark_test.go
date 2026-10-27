package tql

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func benchDocument(b *testing.B, source string) *Document {
	b.Helper()
	doc, err := Parse([]byte(source), "json")
	if err != nil {
		b.Fatal(err)
	}
	return doc
}

func cloneDocument(d *Document) *Document {
	return &Document{source: d.source, root: d.root, format: d.format}
}

func benchServices() string {
	var source strings.Builder
	source.WriteString(`{"services":[`)
	for i := 0; i < 1000; i++ {
		if i > 0 {
			source.WriteByte(',')
		}
		fmt.Fprintf(&source, `{"name":"service-%d","port":%d,"enabled":true}`, i, 8000+i)
	}
	source.WriteString(`]}`)
	return source.String()
}

func benchmarkEngine(b *testing.B, source, program string) {
	base := benchDocument(b, source)
	p, err := Compile(program)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc := cloneDocument(base)
		if _, err := doc.executeVM(p, false, execOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVMCompiledRead(b *testing.B) {
	benchmarkEngine(b, benchServices(), `services[name == "service-500"].port`)
}

func BenchmarkVMScriptFilter(b *testing.B) {
	benchmarkEngine(b, benchServices(), `services[] | select(.enabled) | .port`)
}

func benchLoopSource() string {
	var b strings.Builder
	b.WriteString(`{"a":[`)
	for i := 0; i < 128; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprint(&b, i)
	}
	b.WriteString(`]}`)
	return b.String()
}

func BenchmarkVMScriptForLoop(b *testing.B) {
	benchmarkEngine(b, benchLoopSource(), `for $x in a[] { $x += 1 } a`)
}

func BenchmarkVMScriptWalk(b *testing.B) {
	benchmarkEngine(b, `{"a":{"b":[1,2,3],"c":{"d":4}}}`, `for $x in walk(.) { if type($x) == "number" { $x += 1 } } a`)
}

func BenchmarkVMOneShotEdit(b *testing.B) {
	source := []byte(benchServices())
	p, err := Compile(`services[500].port = 9999`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		doc, e := Parse(source, "json")
		if e != nil {
			b.Fatal(e)
		}
		if _, e = doc.executeVM(p, false, execOptions{}); e != nil {
			b.Fatal(e)
		}
	}
}

func BenchmarkCompileProgram(b *testing.B) {
	const source = `let $n = 0; for $x in a[] { $x += 1; $n += 1 } $n`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(source); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoadProgram(b *testing.B) {
	p, err := Compile(`let $n = 0; for $x in a[] { $x += 1; $n += 1 } $n`)
	if err != nil {
		b.Fatal(err)
	}
	image, err := p.MarshalBinary()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := LoadProgram(image); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDisassembleProgram(b *testing.B) {
	p, err := Compile(`let $n = 0; for $x in a[] { $x += 1; $n += 1 } $n`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if p.Disassemble() == "" {
			b.Fatal("empty disassembly")
		}
	}
}

func BenchmarkAssembleProgram(b *testing.B) {
	p, err := Compile(`let $n = 0; for $x in a[] { $x += 1; $n += 1 } $n`)
	if err != nil {
		b.Fatal(err)
	}
	text := p.Disassemble()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Assemble(text); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVMScriptReadLoop(b *testing.B) {
	benchmarkEngine(b, `{"a":[1,2,3,4,5,6,7,8,9,10]}`, `for $x in a[] { $x * 2 }`)
}

func BenchmarkVMScriptNestedReadLoops(b *testing.B) {
	benchmarkEngine(b, `{"a":[1,2,3,4,5,6,7,8,9,10]}`, `for $x in a[] { for $y in $.a[] { $x * $y } }`)
}

func openAPISource(schemas int) string {
	var b strings.Builder
	b.WriteString(`{"openapi":"3.0.3","info":{"title":"bench","version":"1"},"paths":{},"components":{"schemas":{`)
	for i := 0; i < schemas; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"S%d":{"type":"object","properties":{"id":{"type":"integer","minimum":0,"exclusiveMinimum":true},"name":{"type":"string","nullable":true}}}`, i)
	}
	b.WriteString(`}}}`)
	return b.String()
}

func openAPIScript(b *testing.B) string {
	b.Helper()
	data, err := os.ReadFile("examples/recipes/openapi31/migrate.tql")
	if err != nil {
		b.Fatal(err)
	}
	return string(data)
}

func BenchmarkVMOpenAPI31(b *testing.B) {
	benchmarkEngine(b, openAPISource(50), openAPIScript(b))
}

func openAPICheckScript(b *testing.B) string {
	b.Helper()
	data, err := os.ReadFile("examples/recipes/openapi31/check.tql")
	if err != nil {
		b.Fatal(err)
	}
	return string(data)
}

func BenchmarkVMOpenAPI31Check(b *testing.B) {
	benchmarkEngine(b, openAPISource(50), openAPICheckScript(b))
}
