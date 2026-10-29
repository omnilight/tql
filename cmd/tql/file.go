package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

func atomicWrite(path string, before, after []byte, original os.FileInfo) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tql-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer func() { _ = os.Remove(temp) }()
	if err = f.Chmod(original.Mode().Perm()); err != nil {
		_ = f.Close()
		return err
	}
	if _, err = f.Write(after); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !current.Mode().IsRegular() || !os.SameFile(original, current) {
		return fmt.Errorf("input file changed while query was running")
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, before) {
		return fmt.Errorf("input file changed while query was running")
	}
	return os.Rename(temp, path)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tql-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer func() { _ = os.Remove(temp) }()
	if err = f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
