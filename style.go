package tql

import (
	"fmt"

	"github.com/vasileknik76/tql/syntax"
)

// Style controls newly generated YAML and TOML syntax. Its zero value is auto.
type Style = syntax.Style

// Option configures a Document at parse time.
type Option func(*parseOptions) error
type parseOptions struct{ style Style }

// WithStyle sets a document's generation policy. It does not reformat source.
// Each call replaces the previous policy. Options are document-local.
func WithStyle(style Style) Option {
	return func(o *parseOptions) error { o.style = style; return nil }
}

func configuredFormat(format Format, options []Option) (Format, error) {
	var o parseOptions
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("nil parse option")
		}
		if err := option(&o); err != nil {
			return nil, err
		}
	}
	if len(options) == 0 {
		return format, nil
	}
	f, ok := format.(syntax.StyledFormat)
	if !ok {
		if o.style.IsAuto() {
			return format, nil
		}
		return nil, fmt.Errorf("format does not support explicit styles")
	}
	configured, err := f.WithStyle(o.style)
	if err != nil {
		return nil, err
	}
	if configured == nil {
		return nil, fmt.Errorf("format returned a nil styled backend")
	}
	return configured, nil
}
