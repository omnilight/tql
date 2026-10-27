//go:build !go1.27 || !goexperiment.simd || purego || (!amd64 && !arm64)

package scan

func Quoted(source []byte, quote, escape byte) int { return QuotedScalar(source, quote, escape) }

func LineBreak(source []byte) int { return LineBreakScalar(source) }

func NonASCII(source []byte) int { return NonASCIIScalar(source) }

func YAMLControl(source []byte) int { return YAMLControlScalar(source) }

func YAMLSyntax(source []byte) int { return YAMLSyntaxScalar(source) }
