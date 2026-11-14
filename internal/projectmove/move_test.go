package projectmove

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/managed"
)

type testCopy struct{ fail bool }

func (c testCopy) Copy(_ context.Context, src, dst string) error {
	if c.fail {
		return errors.New("interrupted copy")
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(to, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, to)
		case info.Mode().IsRegular():
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
			if err != nil {
				return err
			}
			_, err = io.Copy(out, in)
			closeErr := out.Close()
			if err != nil {
				return err
			}
			return closeErr
		default:
			return errors.New("unexpected special file")
		}
	})
}
func fixture(t *testing.T) (Service, string, string, []managed.Record) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	src := filepath.Join(root, "source", "my-api")
	volume := filepath.Join(root, "disco-cero-uno")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(volume, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644); err != nil {
		t.Fatal(err)
	}
	s := domain.Snapshot{Containers: []domain.Container{{UUID: "C", Reference: "disk4"}}, Volumes: []domain.Volume{{
		Name: "Projects", UUID: "V", ContainerUUID: "C", Device: "disk4s1", Encrypted: true, MountPoint: volume,
	}}}
	service := Service{Snapshot: func(context.Context) (domain.Snapshot, error) { return s, nil }, Copy: testCopy{}}
	return service, src, volume, []managed.Record{{Name: "Projects", VolumeUUID: "V", ContainerUUID: "C"}}
}
func TestMoveCopiesAndVerifies(t *testing.T) {
	s, src, root, records := fixture(t)
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(canonical, "my-api")
	result, err := s.Move(context.Background(), src, "Projects", records)
	if err != nil {
		t.Fatal(err)
	}
	if result.Destination != dst {
		t.Fatal(result)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git", "HEAD")); err != nil {
		t.Fatal(err)
	}
	items, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("temporary residue remained: %v", items)
	}
	again, err := s.Move(context.Background(), src, "Projects", records)
	if err != nil || !again.Noop {
		t.Fatalf("repeated move must be noop: %+v, %v", again, err)
	}
}
func TestInterruptedCopyPreservesSourceAndCleansStage(t *testing.T) {
	s, src, root, records := fixture(t)
	s.Copy = testCopy{fail: true}
	_, err := s.Move(context.Background(), src, "Projects", records)
	if err == nil {
		t.Fatal("expected copy failure")
	}
	if _, err := os.Stat(filepath.Join(src, ".git", "HEAD")); err != nil {
		t.Fatal(err)
	}
	items, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("staging residue remained: %v", items)
	}
}
func TestExistingDestinationNeverOverwritten(t *testing.T) {
	s, src, root, records := fixture(t)
	if err := os.Mkdir(filepath.Join(root, "my-api"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(context.Background(), src, "Projects", records); err == nil {
		t.Fatal("expected conflict")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal(err)
	}
}
func TestUnmanagedVolumeCannotBeSelected(t *testing.T) {
	s, src, _, _ := fixture(t)
	if _, err := s.Move(context.Background(), src, "Projects", nil); err == nil {
		t.Fatal("unmanaged volume accepted")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal(err)
	}
}
func TestRejectsSourceAlreadyInsideTarget(t *testing.T) {
	s, _, root, records := fixture(t)
	already := filepath.Join(root, "already")
	if err := os.Mkdir(already, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(context.Background(), already, "Projects", records); err == nil {
		t.Fatal("source already on target volume accepted")
	}
}
