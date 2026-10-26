package tql

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	jsonformat "github.com/vasileknik76/tql/formats/json"
	tomlformat "github.com/vasileknik76/tql/formats/toml"
	yamlformat "github.com/vasileknik76/tql/formats/yaml"
)

// Registry maps names to formats. It is safe for concurrent registration and lookup.
// Its zero value is an empty registry ready for use. Do not copy a Registry after
// its first use. Registered formats must be safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	formats map[string]Format
}

// NewRegistry returns an empty format registry.
func NewRegistry() *Registry { return &Registry{formats: make(map[string]Format)} }

// Register adds a format under a case-insensitive name. It rejects empty names,
// nil implementations, and names that are already registered.
func (r *Registry) Register(name string, f Format) error {
	name = formatName(name)
	if name == "" || f == nil {
		return fmt.Errorf("format name and implementation are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.formats == nil {
		r.formats = make(map[string]Format)
	}
	if _, ok := r.formats[name]; ok {
		return fmt.Errorf("format %q is already registered", name)
	}
	r.formats[name] = f
	return nil
}

// Parse parses a copy of source using a format registered under name.
func (r *Registry) Parse(source []byte, name string) (*Document, error) {
	r.mu.RLock()
	f, ok := r.formats[formatName(name)]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown format %q", name)
	}
	return ParseWith(source, f)
}

var defaultRegistry = &Registry{formats: map[string]Format{
	"json": jsonformat.Format{},
	"toml": tomlformat.Format{},
	"yaml": yamlformat.Format{},
	"yml":  yamlformat.Format{},
}}

func formatName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// RegisterFormat adds a format to the default registry; existing names are protected.
func RegisterFormat(name string, f Format) error { return defaultRegistry.Register(name, f) }

// DetectFormat uses a filename extension. Stdin requires an explicit format.
func DetectFormat(filename string) (string, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".json":
		return "json", nil
	case ".yaml", ".yml":
		return "yaml", nil
	case ".toml":
		return "toml", nil
	}
	return "", fmt.Errorf("cannot detect format of %q; specify --format", filename)
}
