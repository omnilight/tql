// The oracle helper exchanges JSON lines with compare.py. It is excluded from
// ordinary builds because it lives under testdata.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/vasileknik76/tql/formats/toml"
	"github.com/vasileknik76/tql/syntax"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	for scanner.Scan() {
		var source string
		if err := json.Unmarshal(scanner.Bytes(), &source); err != nil {
			return fmt.Errorf("decode source: %w", err)
		}
		tree, err := (toml.Format{}).Parse([]byte(source))
		if err != nil {
			response, err := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
			if err != nil {
				return fmt.Errorf("encode error: %w", err)
			}
			if _, err = fmt.Fprintln(out, string(response)); err != nil {
				return fmt.Errorf("write result: %w", err)
			}
			continue
		}
		value, err := syntax.JSON(tree.Interface())
		if err != nil {
			return fmt.Errorf("encode value: %w", err)
		}
		if _, err = fmt.Fprintf(out, "{\"ok\":true,\"value\":%s}\n", value); err != nil {
			return fmt.Errorf("write result: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read source: %w", err)
	}
	return nil
}
