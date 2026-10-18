package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMutationRequiresExistingEmptyMountDirectory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "repos")
	if err := validateWritableMountTarget(missing); err == nil {
		t.Fatal("missing directory must be rejected before mutation")
	}
	if err := os.Mkdir(missing, 0700); err != nil {
		t.Fatal(err)
	}
	if err := validateWritableMountTarget(missing); err != nil {
		t.Fatalf("empty directory should be accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(missing, "existing"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateWritableMountTarget(missing); err == nil {
		t.Fatal("existing data must never be hidden by a mount")
	}
}
