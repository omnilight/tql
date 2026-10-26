// Package tql queries and edits JSON, TOML, and YAML configuration files while
// preserving source formatting outside the requested changes.
//
// Parse creates a Document, Compile prepares a reusable query or script, and
// Document.Exec or Document.Execute applies it atomically. Format implementations
// extend the engine through the source-aware tree and edit protocol in package syntax.
package tql
