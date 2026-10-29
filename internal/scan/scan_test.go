package scan

import (
	"bytes"
	"testing"
)

func TestScanners(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scan    func([]byte) int
		special func(byte) bool
	}{
		{"double", func(s []byte) int { return Quoted(s, '"', '\\') }, func(c byte) bool { return c == '"' || c == '\\' || c < 32 || c == 127 }},
		{"single", func(s []byte) int { return Quoted(s, '\'', '\'') }, func(c byte) bool { return c == '\'' || c < 32 || c == 127 }},
		{"newline", LineBreak, func(c byte) bool { return c == '\r' || c == '\n' }},
		{"yamlplain", YAMLSyntax, func(c byte) bool { return bytes.IndexByte([]byte("\r\n:#[]{},'\""), c) >= 0 }},
		{"unicode", NonASCII, func(c byte) bool { return c >= 127 }},
		{"yamlcontrol", YAMLControl, func(c byte) bool { return c < 32 && c != '\r' && c != '\n' && c != '\t' }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for alignment := range 16 {
				backing := bytes.Repeat([]byte{'a'}, alignment+65)
				s := backing[alignment:]
				for pos := range s {
					for c := range 256 {
						s[pos] = byte(c)
						want := len(s)
						if tc.special(byte(c)) {
							want = pos
						}
						if got := tc.scan(s); got != want {
							t.Fatalf("offset=%d pos=%d byte=%x: got %d want %d", alignment, pos, c, got, want)
						}
					}
					s[pos] = 'a'
				}
				for size := 0; size <= 65; size++ {
					if got := tc.scan(s[:size]); got != size {
						t.Fatalf("size=%d: got %d", size, got)
					}
				}
			}
		})
	}
}

func FuzzScanners(f *testing.F) {
	for _, s := range []string{"", "'quoted'\n", string(bytes.Repeat([]byte{'a'}, 65)), "水😀\x00\x7f\\"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, s []byte) {
		for _, pair := range [][2]int{
			{JSONString(s), JSONStringScalar(s)},
			{Quoted(s, '"', '\\'), QuotedScalar(s, '"', '\\')},
			{Quoted(s, '\'', '\''), QuotedScalar(s, '\'', '\'')},
			{LineBreak(s), LineBreakScalar(s)},
			{NonASCII(s), NonASCIIScalar(s)},
			{YAMLControl(s), YAMLControlScalar(s)},
			{YAMLSyntax(s), YAMLSyntaxScalar(s)},
		} {
			if pair[0] != pair[1] {
				t.Fatalf("SIMD and scalar differ: %v for %q", pair, s)
			}
		}
		if got, want := QuotedString(string(s), '"', '\\'), QuotedScalar(s, '"', '\\'); got != want {
			t.Fatalf("string view: got %d want %d", got, want)
		}
	})
}
