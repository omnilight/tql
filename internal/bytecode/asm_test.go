package bytecode

import (
	"bytes"
	"testing"
)

func TestAssembleRoundTrip(t *testing.T) {
	p := compiled(t, `
param $delta = 2;
let $count = 0;
for $item in items[name == "api"] {
    if $item.enabled { $item.port += $delta; $count += 1 }
}
while $count < 10 { $count += 1 }
services[] | select(.enabled) | .port
`)
	text := Disassemble(p)
	got, err := Assemble(text)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	wantImage, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	gotImage, err := Encode(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wantImage, gotImage) {
		t.Fatalf("assembly round trip differs\n%s", text)
	}
}

func TestAssembleValueKinds(t *testing.T) {
	p := compiled(t, `param $s = "x"; param $a = [1, {b: true}]; let $o = {}; [$s, $a, $o]`)
	text := Disassemble(p)
	got, err := Assemble(text)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	if !bytes.Equal(mustEncode(t, p), mustEncode(t, got)) {
		t.Fatalf("value round trip differs\n%s", text)
	}
}

func mustEncode(t testing.TB, p *Program) []byte {
	t.Helper()
	b, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
