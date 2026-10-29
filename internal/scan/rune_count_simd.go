//go:build go1.27 && goexperiment.simd && !purego && (amd64 || arm64)

package scan

import (
	"math/bits"
	"runtime"
	"simd/archsimd"
	"unicode/utf8"
)

// RuneCount counts UTF-8 code points by subtracting continuation bytes. TQL
// strings are valid UTF-8; invalid input falls back to the scalar path.
func RuneCount(s string) int {
	if len(s) < 32 || runtime.GOARCH == "amd64" && !archsimd.X86.AVX() || !utf8.ValidString(s) {
		return utf8.RuneCountInString(s)
	}
	source := stringBytes(s)
	lower := archsimd.BroadcastUint8x16(0x80)
	upper := archsimd.BroadcastUint8x16(0xc0)
	continuation := 0
	i := 0
	for len(source)-i >= 16 {
		value := archsimd.LoadUint8x16(source[i:])
		match := value.GreaterEqual(lower).And(value.Less(upper))
		words := match.ToInt8x16().ToBits().ReshapeToUint64s()
		// ToBits expands each matching byte to 0xff, so one match is eight bits.
		continuation += (bits.OnesCount64(words.GetElem(0)) + bits.OnesCount64(words.GetElem(1))) / 8
		i += 16
	}
	for ; i < len(source); i++ {
		if source[i]&0xc0 == 0x80 {
			continuation++
		}
	}
	return len(source) - continuation
}
