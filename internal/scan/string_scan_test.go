package scan

import (
	"bytes"
	"testing"
)

func TestIndexStringSpecial(t *testing.T) {
	// Exercise every byte, lane, and tail with unaligned subslices. Bytes >=
	// 0x80 are ordinary here: document-wide UTF-8 validation is separate.
	for alignment := 0; alignment < 16; alignment++ {
		for size := 0; size <= 65; size++ {
			backing := bytes.Repeat([]byte{'a'}, alignment+size+16)
			source := backing[alignment : alignment+size : alignment+size]
			if got := JSONString(source); got != size {
				t.Fatalf("alignment=%d size=%d: got %d", alignment, size, got)
			}
			for pos := range source {
				for c := 0; c <= 255; c++ {
					source[pos] = byte(c)
					want := size
					if c < 0x20 || c == '"' || c == '\\' {
						want = pos
					}
					if got := JSONString(source); got != want {
						t.Fatalf("alignment=%d size=%d pos=%d byte=%x: got %d, want %d", alignment, size, pos, c, got, want)
					}
				}
				source[pos] = 'a'
			}
		}
	}
}

func FuzzIndexStringSpecial(f *testing.F) {
	for _, source := range [][]byte{nil, []byte(`short"tail`), bytes.Repeat([]byte{'a'}, 65), []byte("é水😀\\\x00\"")} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		want := len(source)
		for i, c := range source {
			if c < 0x20 || c == '"' || c == '\\' {
				want = i
				break
			}
		}
		if got := JSONString(source); got != want {
			t.Fatalf("got %d, want %d for %q", got, want, source)
		}
	})
}
