// Command release builds reproducible, dependency-free CLI release archives.
// Run it from the module root: go run ./scripts/release -version v0.1.0.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type target struct{ os, arch, variant string }

var targets = []target{
	{"linux", "amd64", ""},
	{"linux", "arm64", ""},
	{"linux", "386", ""},
	{"linux", "arm", "7"},
	{"darwin", "amd64", ""},
	{"darwin", "arm64", ""},
	{"windows", "amd64", ""},
	{"windows", "arm64", ""},
}

// ZIP timestamps start in 1980. Fixed metadata also makes tar archives stable.
var archiveTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

type archiveFile struct {
	name string
	mode int64
	data []byte
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	version := flags.String("version", "", "semantic version tag, e.g. v0.1.0")
	output := flags.String("output", "dist", "new directory for archives and SHA256SUMS")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if err := validateVersion(*version); err != nil {
		return err
	}
	module, err := os.ReadFile("go.mod")
	if err != nil || !strings.HasPrefix(string(module), "module github.com/vasileknik76/tql\n") {
		return errors.New("run from the TQL module root")
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		return err
	}
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		return err
	}
	outputDir, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(outputDir); err == nil {
		return fmt.Errorf("output already exists: %s", outputDir)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(outputDir), 0755); err != nil {
		return err
	}
	work, err := os.MkdirTemp(filepath.Dir(outputDir), ".tql-release-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	archives := filepath.Join(work, "archives")
	if err = os.Mkdir(archives, 0755); err != nil {
		return err
	}
	for _, t := range targets {
		binaryName := "tql"
		if t.os == "windows" {
			binaryName += ".exe"
		}
		binary := filepath.Join(work, binaryName)
		command := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w -buildid= -X main.version="+*version, "-o", binary, "./cmd/tql")
		command.Env = buildEnvironment(os.Environ(), t)
		if data, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("build %s/%s: %w\n%s", t.os, t.arch, err, data)
		}
		data, err := os.ReadFile(binary)
		if err != nil {
			return err
		}
		name := archiveName(*version, t)
		files := []archiveFile{{binaryName, 0755, data}, {"README.md", 0644, readme}, {"LICENSE", 0644, license}}
		if err := writeArchive(filepath.Join(archives, name), files); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out, name); err != nil {
			return err
		}
	}
	if err := writeChecksums(archives); err != nil {
		return err
	}
	return os.Rename(archives, outputDir)
}

func validateVersion(version string) error {
	if !versionPattern.MatchString(version) {
		return errors.New("version must be a semantic version tag such as v0.1.0")
	}
	if _, pre, ok := strings.Cut(version, "-"); ok {
		for _, identifier := range strings.Split(pre, ".") {
			if len(identifier) > 1 && identifier[0] == '0' && strings.Trim(identifier, "0123456789") == "" {
				return errors.New("numeric prerelease identifiers must not have leading zeroes")
			}
		}
	}
	return nil
}

func archiveName(version string, t target) string {
	arch := t.arch
	if t.variant != "" {
		arch += "v" + t.variant
	}
	extension := ".tar.gz"
	if t.os == "windows" {
		extension = ".zip"
	}
	return "tql_" + strings.TrimPrefix(version, "v") + "_" + t.os + "_" + arch + extension
}

func buildEnvironment(base []string, t target) []string {
	// Ignore host-specific compilation flags, workspaces, and architecture levels.
	overrides := map[string]string{
		"CGO_ENABLED": "0", "GOOS": t.os, "GOARCH": t.arch,
		"GOARM": "7", "GOAMD64": "v1", "GOARM64": "v8.0", "GO386": "sse2",
		"GOEXPERIMENT": "", "GOFLAGS": "", "GOWORK": "off",
	}
	var env []string
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, exists := overrides[strings.ToUpper(key)]; !exists {
			env = append(env, entry)
		}
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+overrides[key])
	}
	return env
}

func writeArchive(path string, files []archiveFile) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if strings.HasSuffix(path, ".zip") {
		err = writeZip(file, files)
	} else {
		err = writeTar(file, files)
	}
	return errors.Join(err, file.Close())
}

func writeTar(out io.Writer, files []archiveFile) error {
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for _, file := range files {
		header := &tar.Header{Name: file.name, Mode: file.mode, Size: int64(len(file.data)), ModTime: archiveTime, Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tw.Write(file.data); err != nil {
			return err
		}
	}
	return errors.Join(tw.Close(), gz.Close())
}

func writeZip(out io.Writer, files []archiveFile) error {
	zw := zip.NewWriter(out)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate, Modified: archiveTime}
		header.SetMode(os.FileMode(file.mode))
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := writer.Write(file.data); err != nil {
			return err
		}
	}
	return zw.Close()
}

func writeChecksums(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var sums strings.Builder
	for _, entry := range entries { // ReadDir returns entries sorted by filename.
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(data), entry.Name())
	}
	return os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums.String()), 0644)
}
