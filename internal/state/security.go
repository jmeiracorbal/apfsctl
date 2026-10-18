package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// privateDir verifies trusted parent directories before creating state files.
// Operations must run as root; state must be private and root-owned.
func privateDir(path string) error {
	if os.Geteuid() != 0 {
		return errors.New("state mutation requires root privileges")
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("state directory must be an absolute clean path")
	}
	for current := path; current != "/"; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe state directory component %q", current)
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 {
			return fmt.Errorf("state directory component %q is not root-owned", current)
		}
		if current == path && info.Mode().Perm()&0077 != 0 {
			return errors.New("state directory must have mode 0700")
		}
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || st.Uid != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("state directory must be private and root-owned")
	}
	return nil
}
