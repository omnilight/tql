// Package scan provides bounded byte scans for parsers and renderers.
package scan

func QuotedScalar(source []byte, quote, escape byte) int {
	for i, c := range source {
		if c == quote || c == escape || c < 0x20 || c == 0x7f {
			return i
		}
	}
	return len(source)
}

func LineBreakScalar(source []byte) int {
	for i, c := range source {
		if c == '\r' || c == '\n' {
			return i
		}
	}
	return len(source)
}

func NonASCIIScalar(source []byte) int {
	for i, c := range source {
		if c >= 0x7f {
			return i
		}
	}
	return len(source)
}

func YAMLControlScalar(source []byte) int {
	for i, c := range source {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			return i
		}
	}
	return len(source)
}

// YAMLSyntax stops at every byte that can affect YAML plain-scalar or key syntax.
func YAMLSyntaxScalar(source []byte) int {
	for i, c := range source {
		switch c {
		case '\r', '\n', ':', '#', '[', ']', '{', '}', ',', '\'', '"':
			return i
		}
	}
	return len(source)
}
