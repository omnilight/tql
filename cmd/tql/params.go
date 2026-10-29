package main

import (
	"fmt"
	"maps"
	"os"
	"strings"

	jsonformat "github.com/vasileknik76/tql/formats/json"
	"github.com/vasileknik76/tql/internal/query"
)

type parameterFlags struct {
	file   string
	values map[string]any
}

func (p *parameterFlags) setFile(path string) error {
	if p.file != "" {
		return fmt.Errorf("--params-file may only be used once")
	}
	if path == "" || path == "-" {
		return fmt.Errorf("--params-file requires a file path; stdin is reserved for the input document")
	}
	p.file = path
	return nil
}

func (p *parameterFlags) set(arg string, json bool) error {
	name, text, ok := strings.Cut(arg, "=")
	if !ok || !query.ValidVariableName(name) {
		return fmt.Errorf("expected NAME=VALUE with a valid parameter name")
	}
	if _, exists := p.values[name]; exists {
		return fmt.Errorf("duplicate command-line parameter %q", name)
	}
	var value any = text
	if json {
		var err error
		value, err = parameterJSON([]byte(text))
		if err != nil {
			return fmt.Errorf("parameter $%s: %w", name, err)
		}
	}
	if p.values == nil {
		p.values = make(map[string]any)
	}
	p.values[name] = value
	return nil
}

func parameterJSON(source []byte) (any, error) {
	node, err := (jsonformat.Format{}).Parse(source)
	if err != nil {
		return nil, err
	}
	return node.Interface(), nil
}

func (p *parameterFlags) load() (map[string]any, error) {
	values := make(map[string]any)
	if p.file != "" {
		source, err := os.ReadFile(p.file)
		if err != nil {
			return nil, err
		}
		value, err := parameterJSON(source)
		if err != nil {
			return nil, fmt.Errorf("parameters file %q: %w", p.file, err)
		}
		var ok bool
		values, ok = value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("parameters file %q must contain a JSON object", p.file)
		}
	}
	maps.Copy(values, p.values)
	return values, nil
}
