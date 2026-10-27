package bytecode

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/vasileknik76/tql/internal/query"
)

func compiled(tb testing.TB, source string) *Program {
	tb.Helper()
	ast, err := query.Parse(source)
	if err != nil {
		tb.Fatal(err)
	}
	p, err := Compile(ast)
	if err != nil {
		tb.Fatal(err)
	}
	return p
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	p := compiled(t, `
param $delta = 2;
let $count = 0;
for $item in items[name == "api"] {
    if $item.enabled { $item.port += $delta; $count += 1 }
}
while $count < 10 { $count += 1 }
$count
`)
	image, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(image)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, got) {
		t.Fatalf("round trip mismatch:\nwant %#v\ngot  %#v", p, got)
	}
	again, err := Encode(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(image, again) {
		t.Fatal("encoding is not deterministic")
	}
}

func TestDecodeRejectsCorruption(t *testing.T) {
	p := compiled(t, `a[0] = 1; a`)
	image, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"short":   image[:10],
		"magic":   append([]byte("BAD!"), image[4:]...),
		"version": append([]byte{image[0], image[1], image[2], image[3], 9, 0}, image[6:]...),
	}
	corrupt := append([]byte(nil), image...)
	corrupt[len(corrupt)-1] ^= 0xff
	cases["checksum"] = corrupt
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(data); err == nil {
				t.Fatal("corrupt image accepted")
			}
		})
	}
}

func TestEncodeEmptyObjectKeys(t *testing.T) {
	p := compiled(t, `let $x = {}; $x`)
	image, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(image); err != nil {
		t.Fatal(err)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte("TQLC"))
	f.Add(mustEncode(f, compiled(f, `a`)))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = Decode(data)
	})
}

func FuzzAssemble(f *testing.F) {
	f.Add(".version 1\n.code C0 \"main\" 0\nHALT\n")
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 1<<16 {
			t.Skip()
		}
		_, _ = Assemble(text)
	})
}
