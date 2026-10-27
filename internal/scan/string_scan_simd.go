//go:build go1.27 && goexperiment.simd && !purego && (amd64 || arm64)

package scan

import (
	"math/bits"
	"runtime"
	"simd/archsimd"
)

// JSONString scans complete 16-byte vectors, then a scalar tail. Loads
// stay within the slice, including for unaligned input and truncated strings.
// ARM64 NEON is mandatory; AMD64's 128-bit archsimd operations require AVX.
func JSONString(source []byte) int {
	if runtime.GOARCH == "amd64" && !archsimd.X86.AVX() {
		return JSONStringScalar(source)
	}
	quote := archsimd.BroadcastUint8x16('"')
	escape := archsimd.BroadcastUint8x16('\\')
	control := archsimd.BroadcastUint8x16(0x20)
	i := 0
	for len(source)-i >= 16 {
		v := archsimd.LoadUint8x16(source[i:])
		match := v.Equal(quote).Or(v.Equal(escape)).Or(v.Less(control))
		// Both supported architectures are little endian. Each matching lane
		// contains 0xff, so trailing zero bits locate the first matching byte.
		words := match.ToInt8x16().ToBits().ReshapeToUint64s()
		lo, hi := words.GetElem(0), words.GetElem(1)
		if lo != 0 {
			return i + bits.TrailingZeros64(lo)/8
		}
		if hi != 0 {
			return i + 8 + bits.TrailingZeros64(hi)/8
		}
		i += 16
	}
	return i + JSONStringScalar(source[i:])
}
