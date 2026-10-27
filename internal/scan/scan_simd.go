//go:build go1.27 && goexperiment.simd && !purego && (amd64 || arm64)

package scan

import (
	"math/bits"
	"runtime"
	"simd/archsimd"
)

// Quoted scans complete vectors only; the remaining bytes use the scalar path.
func Quoted(source []byte, quote, escape byte) int {
	if len(source) < 32 || runtime.GOARCH == "amd64" && !archsimd.X86.AVX() {
		return QuotedScalar(source, quote, escape)
	}
	// A scalar prefix in the parser may end exactly before a closing quote,
	// escape, or line break. Avoid preparing vectors for that zero-length run.
	if c := source[0]; c == quote || c == escape || c < 0x20 || c == 0x7f {
		return 0
	}
	q := archsimd.BroadcastUint8x16(quote)
	e := archsimd.BroadcastUint8x16(escape)
	control := archsimd.BroadcastUint8x16(0x20)
	del := archsimd.BroadcastUint8x16(0x7f)
	i := 0
	for len(source)-i >= 16 {
		v := archsimd.LoadUint8x16(source[i:])
		match := v.Equal(q).Or(v.Equal(e)).Or(v.Less(control)).Or(v.Equal(del))
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
	return i + QuotedScalar(source[i:], quote, escape)
}

// LineBreak scans complete vectors only; the remaining bytes use the scalar path.
func LineBreak(source []byte) int {
	if len(source) < 32 || runtime.GOARCH == "amd64" && !archsimd.X86.AVX() {
		return LineBreakScalar(source)
	}
	cr := archsimd.BroadcastUint8x16('\r')
	lf := archsimd.BroadcastUint8x16('\n')
	i := 0
	for len(source)-i >= 16 {
		v := archsimd.LoadUint8x16(source[i:])
		match := v.Equal(cr).Or(v.Equal(lf))
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
	return i + LineBreakScalar(source[i:])
}

// NonASCII scans complete vectors only; the remaining bytes use the scalar path.
func NonASCII(source []byte) int {
	if len(source) < 32 || runtime.GOARCH == "amd64" && !archsimd.X86.AVX() {
		return NonASCIIScalar(source)
	}
	limit := archsimd.BroadcastUint8x16(0x7f)
	i := 0
	for len(source)-i >= 16 {
		v := archsimd.LoadUint8x16(source[i:])
		match := v.GreaterEqual(limit)
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
	return i + NonASCIIScalar(source[i:])
}

// YAMLControl scans complete vectors only; the remaining bytes use the scalar path.
func YAMLControl(source []byte) int {
	if len(source) < 32 || runtime.GOARCH == "amd64" && !archsimd.X86.AVX() {
		return YAMLControlScalar(source)
	}
	control := archsimd.BroadcastUint8x16(0x20)
	tab := archsimd.BroadcastUint8x16('\t')
	lf := archsimd.BroadcastUint8x16('\n')
	cr := archsimd.BroadcastUint8x16('\r')
	i := 0
	for len(source)-i >= 16 {
		v := archsimd.LoadUint8x16(source[i:])
		allowed := v.Equal(tab).Or(v.Equal(lf)).Or(v.Equal(cr)).ToInt8x16().ToBits()
		match := v.Less(control).ToInt8x16().ToBits().AndNot(allowed)
		words := match.ReshapeToUint64s()
		lo, hi := words.GetElem(0), words.GetElem(1)
		if lo != 0 {
			return i + bits.TrailingZeros64(lo)/8
		}
		if hi != 0 {
			return i + 8 + bits.TrailingZeros64(hi)/8
		}
		i += 16
	}
	return i + YAMLControlScalar(source[i:])
}

func YAMLSyntax(source []byte) int {
	if len(source) < 32 || runtime.GOARCH == "amd64" && !archsimd.X86.AVX() {
		return YAMLSyntaxScalar(source)
	}
	cr := archsimd.BroadcastUint8x16('\r')
	lf := archsimd.BroadcastUint8x16('\n')
	colon := archsimd.BroadcastUint8x16(':')
	hash := archsimd.BroadcastUint8x16('#')
	left := archsimd.BroadcastUint8x16('[')
	right := archsimd.BroadcastUint8x16(']')
	lbrace := archsimd.BroadcastUint8x16('{')
	rbrace := archsimd.BroadcastUint8x16('}')
	comma := archsimd.BroadcastUint8x16(',')
	single := archsimd.BroadcastUint8x16('\'')
	double := archsimd.BroadcastUint8x16('"')
	i := 0
	for len(source)-i >= 16 {
		v := archsimd.LoadUint8x16(source[i:])
		match := v.Equal(cr).Or(v.Equal(lf)).Or(v.Equal(colon)).Or(v.Equal(hash)).Or(v.Equal(left)).Or(v.Equal(right)).Or(v.Equal(lbrace)).Or(v.Equal(rbrace)).Or(v.Equal(comma)).Or(v.Equal(single)).Or(v.Equal(double))
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
	return i + YAMLSyntaxScalar(source[i:])
}
