package policy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jmeiracorbal/verbose-octo-spork/internal/mount"
	"github.com/jmeiracorbal/verbose-octo-spork/internal/system"
)

type MacOS struct{ Run system.Runner }

func (m MacOS) EnsureFstab(ctx context.Context, uuid, path string) (bool, error) {
	return mount.Ensure(ctx, mount.Entry{UUID: uuid, Path: path})
}

func (m MacOS) SpotlightEnabled(ctx context.Context, path string) (bool, error) {
	if m.Run == nil { return false, errors.New("no command runner") }
	output, err := m.Run.Run(ctx, "/usr/bin/mdutil", nil, "-s", path)
	if err != nil { return false, err }
	s := strings.ToLower(string(output))
	switch {
	case strings.Contains(s, "indexing enabled."):
		return true, nil
	case strings.Contains(s, "indexing disabled."):
		return false, nil
	default:
		return false, fmt.Errorf("cannot identify Spotlight status safely: %q", s)
	}
}

func (m MacOS) DisableSpotlight(ctx context.Context, path string) error {
	_, err := m.Run.Run(ctx, "/usr/bin/mdutil", nil, "-i", "off", path)
	return err
}

func pathInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil { return nil, err }
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("volume mount path is not a real directory")
	}
	return info, nil
}

func (m MacOS) FSEventsDisabled(path string) (bool, error) {
	if _, err := pathInfo(path); err != nil { return false, err }
	dir := filepath.Join(path, ".fseventsd")
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) { return false, nil }
	if err != nil { return false, err }
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("unsafe FSEvents directory")
	}
	flag := filepath.Join(dir, "no_log")
	fi, err := os.Lstat(flag)
	if errors.Is(err, os.ErrNotExist) { return false, nil }
	if err != nil { return false, err }
	if !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("unsafe FSEvents marker")
	}
	return true, nil
}

func (m MacOS) DisableFSEvents(path string) error {
	if _, err := pathInfo(path); err != nil { return err }
	dir := filepath.Join(path, ".fseventsd")
	if err := os.Mkdir(dir, 0755); err != nil && !errors.Is(err, os.ErrExist) { return err }
	info, err := os.Lstat(dir)
	if err != nil { return err }
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 { return errors.New("unsafe FSEvents directory") }
	marker := filepath.Join(dir, "no_log")
	fd, err := syscall.Open(marker, syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if errors.Is(err, syscall.EEXIST) {
		ok, e := m.FSEventsDisabled(path)
		if e != nil { return e }
		if ok { return nil }
		return errors.New("FSEvents marker conflict")
	}
	if err != nil { return err }
	return syscall.Close(fd)
}

func (m MacOS) OwnerUID(path string) (int, error) {
	fi, err := pathInfo(path)
	if err != nil { return 0, err }
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok { return 0, errors.New("cannot inspect owner UID") }
	return int(stat.Uid), nil
}

func (m MacOS) ChangeOwner(path string, uid int) error {
	if _, err := pathInfo(path); err != nil { return err }
	return os.Chown(path, uid, -1)
}
