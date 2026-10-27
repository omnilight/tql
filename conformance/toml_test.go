package conformance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vasileknik76/tql/formats/toml"
	"github.com/vasileknik76/tql/syntax"
)

func TestTOMLTestSuite(t *testing.T) {
	const root = "testdata/toml/"
	files := strings.Fields(string(read(t, root+"files-toml-1.1.0")))
	if len(files) != 895 {
		t.Fatalf("incomplete TOML 1.1 manifest: got %d entries, want 895", len(files))
	}
	cases := 0
	for _, file := range files {
		if !strings.HasSuffix(file, ".toml") {
			continue
		}
		cases++
		t.Run(file, func(t *testing.T) {
			source := read(t, root+file)
			original := bytes.Clone(source)
			doc, err := (toml.Format{}).Parse(source)
			if !bytes.Equal(source, original) {
				t.Fatal("parser mutated source")
			}
			if strings.HasPrefix(file, "invalid/") {
				if err == nil || doc != nil {
					t.Fatal("accepted invalid TOML")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var want any
			if err := json.Unmarshal(read(t, root+strings.TrimSuffix(file, ".toml")+".json"), &want); err != nil {
				t.Fatal(err)
			}
			if err := compareTOML(want, doc); err != nil {
				t.Fatal(err)
			}
		})
	}
	if cases != 681 {
		t.Fatalf("incomplete TOML 1.1 corpus: got %d cases, want 681", cases)
	}
}

func compareTOML(want any, got *syntax.Node) error {
	if got == nil {
		return fmt.Errorf("missing value")
	}
	switch got.Kind {
	case syntax.Object:
		object, ok := want.(map[string]any)
		if !ok || len(object) != len(got.Entries) {
			return fmt.Errorf("table differs: want %#v, got %#v", want, got.Interface())
		}
		for _, entry := range got.Entries {
			if err := compareTOML(object[entry.Key], entry.Value); err != nil {
				return fmt.Errorf("%s: %w", entry.Key, err)
			}
		}
		return nil
	case syntax.Array:
		array, ok := want.([]any)
		if !ok || len(array) != len(got.Items) {
			return fmt.Errorf("array differs: want %#v, got %#v", want, got.Interface())
		}
		for i, item := range got.Items {
			if err := compareTOML(array[i], item); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
		return nil
	}
	scalar, ok := want.(map[string]any)
	if !ok {
		return fmt.Errorf("expected scalar reference, got %#v", want)
	}
	kind, _ := scalar["type"].(string)
	value, _ := scalar["value"].(string)
	matched := false
	switch kind {
	case "string":
		matched = got.Kind == syntax.String && got.Value == value
	case "bool":
		matched = got.Kind == syntax.Bool && strconv.FormatBool(got.Value.(bool)) == value
	case "integer":
		matched = got.Kind == syntax.NumberKind && string(got.Value.(syntax.Number)) == value
	case "float":
		if got.Kind == syntax.NumberKind {
			actual := string(got.Value.(syntax.Number))
			a, aerr := tomlFloat(actual)
			b, berr := tomlFloat(value)
			// TOML preserves float vs integer and the sign of zero. NaN signs are immaterial.
			matched = strings.ContainsAny(actual, ".eE") || strings.Contains(actual, "inf") || strings.Contains(actual, "nan")
			matched = matched && aerr == nil && berr == nil && ((math.IsNaN(a) && math.IsNaN(b)) || (a == b && math.Signbit(a) == math.Signbit(b)))
		}
	case "datetime", "datetime-local", "date-local", "time-local":
		if got.Kind == syntax.Datetime {
			a, aerr := tomlTime(kind, string(got.Value.(syntax.Temporal)))
			b, berr := tomlTime(kind, value)
			matched = aerr == nil && berr == nil && a.Equal(b)
		}
	}
	if !matched {
		return fmt.Errorf("want %s %q; got %s %#v", kind, value, got.Kind, got.Value)
	}
	return nil
}

func tomlTime(kind, value string) (time.Time, error) {
	value = strings.NewReplacer("t", "T", "z", "Z", " ", "T").Replace(value)
	clock := 0
	if strings.Contains(value, "T") {
		clock = 11
	}
	if len(value) >= clock+5 && value[clock+2] == ':' && (len(value) == clock+5 || value[clock+5] != ':') {
		value = value[:clock+5] + ":00" + value[clock+5:]
	}
	layout := map[string]string{
		"datetime":       time.RFC3339Nano,
		"datetime-local": "2006-01-02T15:04:05.999999999",
		"date-local":     "2006-01-02",
		"time-local":     "15:04:05.999999999",
	}[kind]
	return time.Parse(layout, value)
}

func tomlFloat(value string) (float64, error) {
	if strings.TrimLeft(value, "+-") == "nan" {
		return math.NaN(), nil
	}
	return strconv.ParseFloat(value, 64)
}
