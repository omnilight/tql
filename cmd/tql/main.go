// Command tql queries and edits JSON, TOML and YAML configuration files.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/vasileknik76/tql"
	"github.com/vasileknik76/tql/syntax"
)

var version = "dev"

const usage = `Usage: tql [options] 'query' [file|-]
       tql [options] --query-file program.tql [file|-]

Read JSON, TOML or YAML; change only requested source ranges.
A read prints one JSON value per match. A mutation prints the edited source.
Flags precede the query. Stdin requires --format.

Examples:
  tql 'server.port' config.toml
  tql -i 'server.port = 8080' config.toml
  tql -f yaml 'users[name == "alice"].enabled = true' config.yaml
  cat config.json | tql -f json 'items[] | select(.enabled) | .name'

Options:
  -f, --format NAME     Input format: json, toml, yaml (yml)
  -i, --in-place        Atomically replace a regular input file (mutations only)
  -r, --raw             Print read strings without JSON quotes
      --values          Print read results even when the query also mutates
      --check           No output/write; exit 1 if the query would change source
      --query-file FILE Read the query from a file
      --version         Print version
  -h, --help            Show this help

Exit status: 0 success; 1 --check detected changes; 2 query/input/write error.
`

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
func run(args []string, in io.Reader, out, stderr io.Writer) int {
	fs := flag.NewFlagSet("tql", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var format, queryFile string
	var inPlace, raw, values, check, showVersion, help bool
	fs.StringVar(&format, "f", "", "input format")
	fs.StringVar(&format, "format", "", "input format")
	fs.BoolVar(&inPlace, "i", false, "edit in place")
	fs.BoolVar(&inPlace, "in-place", false, "edit in place")
	fs.BoolVar(&raw, "r", false, "raw read strings")
	fs.BoolVar(&raw, "raw", false, "raw read strings")
	fs.BoolVar(&values, "values", false, "print read results")
	fs.BoolVar(&check, "check", false, "check for changes")
	fs.StringVar(&queryFile, "query-file", "", "query file")
	fs.BoolVar(&showVersion, "version", false, "show version")
	fs.BoolVar(&help, "h", false, "show help")
	fs.BoolVar(&help, "help", false, "show help")
	fs.Usage = func() { _, _ = fmt.Fprint(stderr, usage) }
	fail := func(err error) int { _, _ = fmt.Fprintf(stderr, "tql: %v\n", err); return 2 }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if help {
		_, err := fmt.Fprint(out, usage)
		if err != nil {
			return fail(err)
		}
		return 0
	}
	if showVersion {
		_, err := fmt.Fprintln(out, "tql "+version)
		if err != nil {
			return fail(err)
		}
		return 0
	}
	positional := fs.Args()
	var program string
	if queryFile != "" {
		data, err := os.ReadFile(queryFile)
		if err != nil {
			return fail(err)
		}
		program = string(data)
	} else {
		if len(positional) == 0 {
			fs.Usage()
			return fail(fmt.Errorf("query is required"))
		}
		program = positional[0]
		positional = positional[1:]
	}
	if len(positional) > 1 {
		return fail(fmt.Errorf("expected one input file"))
	}
	filename := "-"
	if len(positional) == 1 {
		filename = positional[0]
	}
	if inPlace && check {
		return fail(fmt.Errorf("--in-place and --check cannot be combined"))
	}
	if inPlace && filename == "-" {
		return fail(fmt.Errorf("--in-place requires an input file"))
	}
	p, err := tql.Compile(program)
	if err != nil {
		return fail(err)
	}
	if inPlace && !p.Mutates() {
		return fail(fmt.Errorf("--in-place requires a mutation query"))
	}
	if raw && p.Mutates() && !values {
		return fail(fmt.Errorf("--raw with mutations requires --values"))
	}
	if inPlace && values {
		return fail(fmt.Errorf("--in-place and --values cannot be combined"))
	}
	if format == "" {
		if filename == "-" {
			return fail(fmt.Errorf("stdin requires --format"))
		}
		format, err = tql.DetectFormat(filename)
		if err != nil {
			return fail(err)
		}
	}
	var source []byte
	var original os.FileInfo
	if filename == "-" {
		source, err = io.ReadAll(in)
	} else {
		if inPlace {
			original, err = os.Lstat(filename)
			if err != nil {
				return fail(err)
			}
			if !original.Mode().IsRegular() {
				return fail(fmt.Errorf("--in-place requires a regular file; symlinks are refused"))
			}
		}
		source, err = os.ReadFile(filename)
	}
	if err != nil {
		return fail(err)
	}
	doc, err := tql.Parse(source, format)
	if err != nil {
		return fail(err)
	}
	result, err := doc.Execute(p)
	if err != nil {
		return fail(err)
	}
	if check {
		if result.Changed {
			return 1
		}
		return 0
	}
	if inPlace {
		if result.Changed {
			if err = atomicWrite(filename, source, result.Bytes, original); err != nil {
				return fail(err)
			}
		}
		return 0
	}
	if p.Mutates() && !values {
		_, err = out.Write(result.Bytes)
		if err != nil {
			return fail(err)
		}
		return 0
	}
	for _, v := range result.Values {
		var s string
		if str, ok := v.(string); raw && ok {
			s = str
		} else {
			s, err = syntax.JSON(v)
			if err != nil {
				return fail(err)
			}
		}
		if _, err = fmt.Fprintln(out, s); err != nil {
			return fail(err)
		}
	}
	return 0
}
