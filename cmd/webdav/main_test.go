package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRoots(t *testing.T) {
	root := t.TempDir()
	if err := validateRoots([]string{root}); err != nil {
		t.Fatalf("validateRoots(valid) error = %v", err)
	}

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := validateRoots([]string{file}); err == nil {
		t.Fatal("validateRoots(file) error = nil, want not-a-directory error")
	}
	if err := validateRoots([]string{filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("validateRoots(missing) error = nil, want error")
	}
}
