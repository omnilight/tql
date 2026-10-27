package scan

import "unsafe"

// stringBytes is a read-only view used only by the scanners in this package.
// No view escapes a call, no scanner mutates input, and all loads are bounded by
// len(s). This avoids allocating a copy of a long immutable string for scanning.
func stringBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func QuotedString(s string, quote, escape byte) int { return Quoted(stringBytes(s), quote, escape) }
func NonASCIIString(s string) int                   { return NonASCII(stringBytes(s)) }
