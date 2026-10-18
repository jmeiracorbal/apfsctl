package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type FileLock struct{ Path string }

// Acquire provides nonblocking, host-wide exclusion for managed volumes.
func (l FileLock) Acquire() (func() error, error) {
	if l.Path == "" {
		return nil, errors.New("lock path is empty")
	}
	if err := privateDir(filepath.Dir(l.Path)); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(l.Path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("open operation lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), l.Path)
	if file == nil {
		syscall.Close(fd)
		return nil, errors.New("failed to open lock file")
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != 0 {
		file.Close()
		return nil, errors.New("lock file is not a root-owned regular file")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another devvolume operation is running: %w", err)
	}
	return func() error {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	}, nil
}
