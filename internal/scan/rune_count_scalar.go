//go:build !go1.27 || !goexperiment.simd || purego || (!amd64 && !arm64)

package scan

import "unicode/utf8"

// RuneCount returns the number of Unicode code points in s.
func RuneCount(s string) int { return utf8.RuneCountInString(s) }
