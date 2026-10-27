package scan

// JSONStringScalar finds the first quote, escape, or illegal unescaped
// control byte. UTF-8 is validated once for the complete document by Parse.
// Returning len(source) lets the caller distinguish an unterminated string.
func JSONStringScalar(source []byte) int {
	for i, c := range source {
		if c == '"' || c == '\\' || c < 0x20 {
			return i
		}
	}
	return len(source)
}
