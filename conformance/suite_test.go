// Package conformance_test checks the public parsers against pinned upstream data.
package conformance_test

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func exists(t *testing.T, name string) bool {
	t.Helper()
	_, err := os.Stat(name)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

// Decode a stream, retaining exact numbers and distinguishing booleans from them.
func jsonDocuments(t *testing.T, data []byte) []any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	documents := make([]any, 0)
	for {
		var value any
		err := decoder.Decode(&value)
		if err == io.EOF {
			return documents
		}
		if err != nil {
			t.Fatal(err)
		}
		documents = append(documents, normalizeNumbers(t, value))
	}
}

func normalizeNumbers(t *testing.T, value any) any {
	t.Helper()
	switch v := value.(type) {
	case json.Number:
		return decimal(t, string(v))
	case []any:
		for i, child := range v {
			v[i] = normalizeNumbers(t, child)
		}
	case map[string]any:
		for key, child := range v {
			v[key] = normalizeNumbers(t, child)
		}
	}
	return value
}

func compareJSON(t *testing.T, want, got []byte) {
	t.Helper()
	if !reflect.DeepEqual(jsonDocuments(t, want), jsonDocuments(t, got)) {
		t.Errorf("JSON values differ:\nwant %s\n got %s", want, got)
	}
}

// Canonical decimal coefficient/exponent pairs avoid allocating 10^exponent for
// JSONTestSuite's enormous exponents. Equality never passes through float64.
type decimalValue struct{ coefficient, exponent string }

func decimal(t *testing.T, value string) decimalValue {
	t.Helper()
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == 'e' || r == 'E' })
	exponent := new(big.Int)
	if len(parts) == 2 {
		if _, ok := exponent.SetString(parts[1], 10); !ok {
			t.Fatalf("invalid exponent %q", parts[1])
		}
	}
	coefficient := parts[0]
	if dot := strings.IndexByte(coefficient, '.'); dot >= 0 {
		exponent.Sub(exponent, big.NewInt(int64(len(coefficient)-dot-1)))
		coefficient = coefficient[:dot] + coefficient[dot+1:]
	}
	coefficient = strings.TrimLeft(coefficient, "0")
	if coefficient == "" {
		return decimalValue{"0", "0"}
	}
	trimmed := strings.TrimRight(coefficient, "0")
	exponent.Add(exponent, big.NewInt(int64(len(coefficient)-len(trimmed))))
	if negative {
		trimmed = "-" + trimmed
	}
	return decimalValue{trimmed, exponent.String()}
}
