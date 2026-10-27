// This optional TOML 1.1 reference is isolated from the library's dependencies.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var source string
		if err := json.Unmarshal(scanner.Bytes(), &source); err != nil {
			fatal(err)
		}
		var value map[string]any
		_, err := toml.Decode(source, &value)
		if err != nil {
			if err := encoder.Encode(map[string]any{"ok": false, "error": err.Error()}); err != nil {
				fatal(err)
			}
			continue
		}
		if err := encoder.Encode(map[string]any{"ok": true, "value": tagged(value)}); err != nil {
			fatal(err)
		}
	}
	if err := scanner.Err(); err != nil {
		fatal(err)
	}
}
func tagged(value any) map[string]any {
	kind, text := "", ""
	switch v := value.(type) {
	case map[string]any:
		children := make(map[string]any, len(v))
		for k, child := range v {
			children[k] = tagged(child)
		}
		return map[string]any{"type": "table", "value": children}
	case []any:
		children := make([]any, len(v))
		for i, child := range v {
			children[i] = tagged(child)
		}
		return map[string]any{"type": "array", "value": children}
	case []map[string]any:
		children := make([]any, len(v))
		for i, child := range v {
			children[i] = tagged(child)
		}
		return map[string]any{"type": "array", "value": children}
	case string:
		kind, text = "string", v
	case bool:
		kind, text = "bool", strconv.FormatBool(v)
	case int64:
		kind, text = "integer", strconv.FormatInt(v, 10)
	case float64:
		kind, text = "float", strconv.FormatFloat(v, 'g', -1, 64)
	case time.Time:
		kind, text = "datetime", v.Format(time.RFC3339Nano)
		switch v.Location().String() {
		case "datetime-local":
			kind, text = "datetime-local", v.Format("2006-01-02T15:04:05.999999999")
		case "date-local":
			kind, text = "date-local", v.Format("2006-01-02")
		case "time-local":
			kind, text = "time-local", v.Format("15:04:05.999999999")
		}
	default:
		fatal(fmt.Errorf("unexpected reference value %T", value))
	}
	return map[string]any{"type": kind, "value": text}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
