// Package tql queries and edits JSON, TOML, and YAML configuration files while
// preserving source formatting outside the requested changes.
//
// Parse creates a Document, Compile prepares a reusable query or script, and
// Document.Exec, Document.Execute, or Document.Apply changes it atomically.
// Scripts declare input parameters with param; WithParams binds independent,
// read-only values for one execution, including when reusing compiled programs.
// Package edit builds structured operations, document diffs, and merge patches.
// Format implementations extend the engine through the source-aware tree and edit
// protocol in package syntax.
package tql
