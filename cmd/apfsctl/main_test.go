package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/reconcile"
)

func TestRejectsNonEmptyMountPoint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	p := reconcile.Plan{Changes: []reconcile.Change{{Action: reconcile.Mount}}}
	if err := ensureSafeMountTarget(dir, p); err == nil {
		t.Fatal("must reject nonempty mount point")
	}
}

func TestAllowsExistingConvergedMount(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	p := reconcile.Plan{Desired: domain.Desired{MountPoint: dir}, Noop: true}
	if err := ensureSafeMountTarget(dir, p); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsAncestorSymlink(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	p := reconcile.Plan{Changes: []reconcile.Change{{Action: reconcile.Mount}}}
	if err := ensureSafeMountTarget(filepath.Join(alias, "repos"), p); err == nil {
		t.Fatal("mount point ancestor symlink must fail")
	}
}
