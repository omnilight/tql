package scan

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRuneCount(t *testing.T) {
	values := []string{
		"", "a", "hello, world", "Привет, мир", "café", "😀🙂", "e\u0301",
		strings.Repeat("a", 1000), strings.Repeat("я", 1000), strings.Repeat("😀", 1000),
	}
	for _, value := range values {
		if got, want := RuneCount(value), utf8.RuneCountInString(value); got != want {
			t.Fatalf("RuneCount(%q)=%d want %d", value, got, want)
		}
	}
}

func BenchmarkRuneCount(b *testing.B) {
	value := strings.Repeat("configuration-", 1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = RuneCount(value)
	}
}
