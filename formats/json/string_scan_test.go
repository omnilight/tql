package json

import (
	stdjson "encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestStringScanBoundaries(t *testing.T) {
	for size := 0; size <= 65; size++ {
		prefix := strings.Repeat("a", size)
		for _, suffix := range []string{`"`, `\n"`, `\"end"`, `\uD834\uDD1E"`, `水😀"`} {
			source := `"` + prefix + suffix
			n := parse(t, source)
			var want string
			if err := stdjson.Unmarshal([]byte(source), &want); err != nil {
				t.Fatal(err)
			}
			if n.Value != want {
				t.Fatalf("got %q, want %q", n.Value, want)
			}
			if n.Start != 0 || n.End != len(source) {
				t.Fatalf("incorrect string span: %#v", n)
			}
		}
		for _, c := range []byte{0, '\n', 0x1f} {
			source := []byte(`"` + prefix + string(c) + `"`)
			_, err := (Format{}).Parse(source)
			want := fmt.Sprintf("json: unescaped control character in string at line 1, column %d (byte %d)", size+3, size+2)
			if c == '\n' {
				want = fmt.Sprintf("json: unescaped control character in string at line 2, column 1 (byte %d)", size+2)
			}
			if err == nil || err.Error() != want {
				t.Fatalf("got %v, want %s", err, want)
			}
		}
		if _, err := (Format{}).Parse([]byte(`"` + prefix)); err == nil {
			t.Fatal("accepted unterminated string")
		}
	}
}

func TestParsedStringsOwnTheirBytes(t *testing.T) {
	key := strings.Repeat("key", 32)
	value := strings.Repeat("value", 32)
	source := []byte(`{"` + key + `":"` + value + `"}`)
	n, err := (Format{}).Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	for i := range source {
		source[i] = 'x'
	}
	if n.Entries[0].Key != key || n.Entries[0].Value.Value != value {
		t.Fatal("parsed key or string aliases source bytes")
	}
}
