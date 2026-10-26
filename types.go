package tql

import "github.com/vasileknik76/tql/syntax"

// Format is the parsing and source-editing protocol for configuration formats.
type Format = syntax.Format

// Number stores an exact decimal number without float64 rounding.
type Number = syntax.Number

// Temporal stores a TOML date or time separately from an ordinary string.
type Temporal = syntax.Temporal
