package syntax

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestEditValidation(t *testing.T) {
	source := []byte("α=1 β=2")
	out, e := Apply(source, []Edit{{Start: 8, End: 9, Text: []byte("30")}, {Start: 3, End: 4, Text: []byte("20")}})
	if e != nil {
		t.Fatal(e)
	}
	if string(out) != "α=20 β=30" {
		t.Fatalf("out=%q", out)
	}
	if string(source) != "α=1 β=2" {
		t.Fatal("mutated input")
	}
	for _, edits := range [][]Edit{{{Start: -1, End: 0}}, {{Start: 0, End: 100}}, {{Start: 3, End: 2}}, {{Start: 1, End: 4}, {Start: 2, End: 2}}, {{Start: 3, End: 4}, {Start: 3, End: 5}}} {
		if _, e := Apply(source, edits); e == nil {
			t.Fatalf("accepted invalid edits %#v", edits)
		}
	}
}

func TestApplyStableInsertionOrderAndOwnership(t *testing.T) {
	source := []byte("abc")
	edits := []Edit{
		{Start: 3, End: 3, Text: []byte("!")},
		{Start: 1, End: 1, Text: []byte("first")},
		{Start: 1, End: 1, Text: []byte("second")},
		{Start: 1, End: 2, Text: []byte("B")},
	}
	out, err := Apply(source, edits)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "afirstsecondBc!" {
		t.Fatalf("out = %q", out)
	}
	if edits[0].Start != 3 {
		t.Fatal("Apply sorted caller-owned edits")
	}
	out[0] = 'A'
	if string(source) != "abc" {
		t.Fatal("output aliases source")
	}
	copy, err := Apply(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	copy[0] = 'A'
	if string(source) != "abc" {
		t.Fatal("no-op output aliases source")
	}
	if out, err := Apply(source, []Edit{{Start: 0, End: len(source)}}); err != nil || len(out) != 0 {
		t.Fatalf("delete document: %q, %v", out, err)
	}
}

func TestLinkedLookups(t *testing.T) {
	for _, size := range []int{0, indexThreshold - 1, indexThreshold, 128} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			object := &Node{Kind: Object}
			array := &Node{Kind: Array}
			for i := 0; i < size; i++ {
				object.Entries = append(object.Entries, &Entry{Key: fmt.Sprint(i), Value: &Node{Kind: Null}})
				array.Items = append(array.Items, &Node{Kind: Null})
			}
			check := func() {
				for i := 0; i < size; i++ {
					if object.Get(fmt.Sprint(i)) != object.Entries[i].Value || object.Entry(object.Entries[i].Value) != object.Entries[i] {
						t.Fatalf("incorrect object lookup at %d", i)
					}
					if array.Index(array.Items[i]) != i {
						t.Fatalf("incorrect array lookup at %d", i)
					}
				}
				if object.Get("missing") != nil || object.Entry(&Node{}) != nil || array.Index(&Node{}) != -1 {
					t.Fatal("missing child was found")
				}
			}
			check() // Parsers can use the same lookup helpers before Link.
			Link(object)
			Link(array)
			check()
			for i := 0; i < size; i++ {
				if object.Entries[i].Value.Parent != object || array.Items[i].Parent != array {
					t.Fatal("parent pointer was not linked")
				}
			}
		})
	}
	var missing *Node
	if missing.Get("x") != nil || missing.Entry(&Node{}) != nil || missing.Index(&Node{}) != -1 {
		t.Fatal("nil tree lookups")
	}
}

func TestLinkRebuildsIndexesAndPreservesFirstMatch(t *testing.T) {
	first, second := &Node{}, &Node{}
	object := &Node{Kind: Object}
	array := &Node{Kind: Array}
	for i := 0; i < indexThreshold; i++ {
		object.Entries = append(object.Entries, &Entry{Key: fmt.Sprint(i), Value: &Node{}})
		array.Items = append(array.Items, &Node{})
	}
	object.Entries[0] = &Entry{Key: "duplicate", Value: first}
	object.Entries[1] = &Entry{Key: "duplicate", Value: second}
	object.Entries[2].Value = first
	array.Items[0], array.Items[1] = first, first
	Link(object)
	Link(array)
	if object.Get("duplicate") != first || object.Entry(first) != object.Entries[0] || array.Index(first) != 0 {
		t.Fatal("indexes changed first-match lookup semantics")
	}
	object.Entries[0].Key = "renamed"
	array.Items[0] = second
	Link(object)
	Link(array)
	if object.Get("duplicate") != second || object.Get("renamed") != first || array.Index(first) != 1 {
		t.Fatal("relinking did not rebuild indexes")
	}
	object.Entries = object.Entries[:1]
	array.Items = array.Items[:1]
	Link(object)
	Link(array)
	if object.Get("duplicate") != nil || object.Get("renamed") != first || array.Index(first) != -1 {
		t.Fatal("stale index survived shrinking tree")
	}
}

func TestLinkedLookupConcurrentReads(t *testing.T) {
	object := &Node{Kind: Object}
	for i := 0; i < 128; i++ {
		object.Entries = append(object.Entries, &Entry{Key: fmt.Sprint(i), Value: &Node{}})
	}
	Link(object)
	var readers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for i := 0; i < 128; i++ {
				if object.Get(fmt.Sprint(i)) != object.Entries[i].Value || object.Entry(object.Entries[i].Value) != object.Entries[i] {
					t.Error("concurrent lookup failed")
					return
				}
			}
		}()
	}
	readers.Wait()
}

func BenchmarkWideNodeLookup(b *testing.B) {
	root := &Node{Kind: Object}
	for i := 0; i < 4096; i++ {
		root.Entries = append(root.Entries, &Entry{Key: fmt.Sprintf("key%d", i), Value: &Node{Kind: NumberKind, Value: Number("1")}})
	}
	Link(root)
	child := root.Entries[len(root.Entries)-1].Value
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if root.Get("key4095") != child || root.Entry(child) == nil {
			b.Fatal("missing child")
		}
	}
}

func BenchmarkApplyManyEdits(b *testing.B) {
	source := []byte(strings.Repeat("abcdefgh", 8192))
	edits := make([]Edit, 256)
	for i := range edits {
		edits[i] = Edit{Start: i * 256, End: i*256 + 1, Text: []byte("1234")}
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Apply(source, edits); err != nil {
			b.Fatal(err)
		}
	}
}
func TestNumberJSON(t *testing.T) {
	for _, tc := range []struct {
		n    Number
		want string
	}{{"9007199254740993", "9007199254740993"}, {"1e100000", "1e100000"}, {"-0.01", "-0.01"}, {"inf", `"inf"`}, {"nan", `"nan"`}} {
		out, e := json.Marshal(tc.n)
		if e != nil {
			t.Fatal(e)
		}
		if string(out) != tc.want {
			t.Fatalf("got %s want %s", out, tc.want)
		}
	}
	for _, n := range []Number{"01", "1.", "0x1", "1/2", "abc", "1]"} {
		if _, e := JSON(n); e == nil {
			t.Fatalf("encoded invalid number %q", n)
		}
	}
}
func FuzzQuote(f *testing.F) {
	for _, s := range []string{"", "line\n\x00\t", "😀 café", "<>&\u2028", "\"\\/"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		encoded := Quote(s)
		var decoded string
		if e := json.Unmarshal([]byte(encoded), &decoded); e != nil {
			t.Fatalf("invalid JSON %q: %v", encoded, e)
		}
	})
}
