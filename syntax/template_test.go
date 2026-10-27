package syntax

import "testing"

func TestNearbyTemplateMajorityAndLastTie(t *testing.T) {
	for _, styles := range [][]string{
		{"a", "b", "b", "a", "b", "a"},
		{"a", "a", "a", "b", "b"},
		{"a", "b", "b", "b", "a", "a", "a"},
	} {
		root := &Node{Kind: Array}
		counts := map[string]int{}
		for _, style := range styles {
			root.Items = append(root.Items, &Node{Kind: String, Style: style})
			counts[style]++
		}
		var want *Node
		best := 0
		for _, n := range root.Items {
			if counts[n.Style] >= best {
				want = n
				best = counts[n.Style]
			}
		}
		calls := 0
		got := NearbyTemplate(root, String, nil, func(n *Node) string { calls++; return n.Style })
		if got != want {
			t.Fatalf("%v: got %v want %v", styles, got, want)
		}
		if calls != len(styles) {
			t.Fatalf("signature called %d times for %d candidates", calls, len(styles))
		}
	}
}
