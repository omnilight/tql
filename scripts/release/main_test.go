package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchivesAreReproducibleAndComplete(t *testing.T) {
	files := []archiveFile{{"tql", 0755, []byte("binary")}, {"README.md", 0644, []byte("readme")}, {"LICENSE", 0644, []byte("license")}}
	for _, extension := range []string{".tar.gz", ".zip"} {
		t.Run(extension, func(t *testing.T) {
			dir := t.TempDir()
			paths := []string{filepath.Join(dir, "first"+extension), filepath.Join(dir, "second"+extension)}
			for _, path := range paths {
				if err := writeArchive(path, files); err != nil {
					t.Fatal(err)
				}
			}
			first, err := os.ReadFile(paths[0])
			if err != nil {
				t.Fatal(err)
			}
			second, err := os.ReadFile(paths[1])
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, second) {
				t.Fatal("archives differ despite identical contents")
			}
			if err := writeArchive(paths[0], files); err == nil {
				t.Fatal("overwrote an existing archive")
			}
			if extension == ".zip" {
				reader, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
				if err != nil {
					t.Fatal(err)
				}
				if len(reader.File) != len(files) {
					t.Fatal("wrong ZIP member count")
				}
				for i, member := range reader.File {
					if member.Name != files[i].name || member.Mode().Perm() != os.FileMode(files[i].mode) || !member.Modified.Equal(archiveTime) {
						t.Fatalf("unexpected ZIP header: %+v", member.FileHeader)
					}
					rc, err := member.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, readErr := io.ReadAll(rc)
					closeErr := rc.Close()
					if readErr != nil || closeErr != nil || !bytes.Equal(data, files[i].data) {
						t.Fatalf("invalid ZIP content: %q, %v, %v", data, readErr, closeErr)
					}
				}
			} else {
				gz, err := gzip.NewReader(bytes.NewReader(first))
				if err != nil {
					t.Fatal(err)
				}
				reader := tar.NewReader(gz)
				for _, file := range files {
					header, err := reader.Next()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(reader)
					if err != nil {
						t.Fatal(err)
					}
					if header.Name != file.name || header.Mode != file.mode || !header.ModTime.Equal(archiveTime) || !bytes.Equal(data, file.data) {
						t.Fatalf("unexpected tar member: %+v, %q", header, data)
					}
				}
				if _, err := reader.Next(); err != io.EOF {
					t.Fatalf("unexpected extra tar member: %v", err)
				}
				if err := gz.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeChecksums(dir); err != nil {
				t.Fatal(err)
			}
			sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("%x  %s\n%x  %s\n", sha256.Sum256(first), filepath.Base(paths[0]), sha256.Sum256(second), filepath.Base(paths[1]))
			if string(sums) != want {
				t.Fatalf("checksums = %q, want %q", sums, want)
			}
		})
	}
}

func TestVersionAndOutputSafety(t *testing.T) {
	for _, version := range []string{"v0.1.0", "v1.2.3-rc.1", "v10.0.0-beta-2"} {
		if err := validateVersion(version); err != nil {
			t.Fatalf("%s: %v", version, err)
		}
	}
	for _, version := range []string{"", "dev", "0.1.0", "v01.1.0", "v1.2", "v1.2.3-01", "v1.2.3;touch /tmp/unwanted", "v1.2.3/../../"} {
		if err := validateVersion(version); err == nil {
			t.Fatalf("accepted unsafe/invalid version %q", version)
		}
	}
	if err := run([]string{"-version", "dev"}, io.Discard); err == nil {
		t.Fatal("accepted an invalid release version")
	}
}

func TestBuildEnvironmentOverridesHostFlags(t *testing.T) {
	env := buildEnvironment([]string{"PATH=/test", "GOOS=wrong", "GOARCH=wrong", "CGO_ENABLED=1", "GOFLAGS=-race", "GOEXPERIMENT=unwanted", "GOWORK=/unwanted"}, target{"linux", "arm", "7"})
	want := map[string]string{"GOOS": "linux", "GOARCH": "arm", "CGO_ENABLED": "0", "GOARM": "7", "GOFLAGS": "", "GOEXPERIMENT": "", "GOWORK": "off", "PATH": "/test"}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if expected, ok := want[key]; ok {
			if value != expected {
				t.Errorf("%s = %q, want %q", key, value, expected)
			}
			delete(want, key)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing variables: %v", want)
	}
	if got := archiveName("v0.1.0", target{"linux", "arm", "7"}); got != "tql_0.1.0_linux_armv7.tar.gz" {
		t.Fatal(got)
	}
	if got := archiveName("v0.1.0", target{"windows", "arm64", ""}); got != "tql_0.1.0_windows_arm64.zip" {
		t.Fatal(got)
	}
}
