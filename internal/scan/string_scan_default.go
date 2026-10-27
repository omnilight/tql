//go:build !go1.27 || !goexperiment.simd || purego || (!amd64 && !arm64)

package scan

func JSONString(source []byte) int {
	return JSONStringScalar(source)
}
