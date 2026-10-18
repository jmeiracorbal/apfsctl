package mount

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Ensure uses vifs to serialize changes with other macOS fstab editors.
// The Go executable is the editor; no shell scripts are generated.
func Ensure(ctx context.Context, e Entry) (bool, error) {
	if err := e.Validate(); err != nil {
		return false, err
	}
	if os.Geteuid() != 0 {
		return false, errors.New("fstab configuration requires root")
	}
	before, err := os.ReadFile("/private/etc/fstab")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	_, changed, err := Merge(before, e)
	if err != nil || !changed {
		return false, err
	}
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return false, err
	}
	if strings.ContainsAny(executable, " \t\r\n'\"\\") {
		return false, fmt.Errorf("unsafe executable path for vifs editor: %q", executable)
	}
	cmd := exec.CommandContext(ctx, "/usr/sbin/vifs")
	cmd.Env = append(os.Environ(), "EDITOR="+executable, "VISUAL="+executable,
		"DEVVOLUME_VIFS_UUID="+e.UUID, "DEVVOLUME_VIFS_PATH="+e.Path)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("vifs: %w: %s", err, output.String())
	}
	after, err := os.ReadFile("/private/etc/fstab")
	if err != nil {
		return false, err
	}
	_, changed, err = Merge(after, e)
	if err != nil {
		return false, err
	}
	if changed {
		return false, errors.New("vifs returned without persisting the desired fstab entry")
	}
	return true, nil
}

// EditTemporary is the native editor entrypoint used by vifs.
// Only root-owned, regular, nonsymlink temporary files are writable.
func EditTemporary(path string, e Entry) error {
	if os.Geteuid() != 0 {
		return errors.New("fstab editor requires root")
	}
	if err := e.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid temporary fstab path")
	}
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	info, ok := stat.Sys().(*syscall.Stat_t)
	if !ok || !stat.Mode().IsRegular() || info.Uid != 0 {
		return errors.New("fstab editor requires a root-owned regular temporary file")
	}
	before, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil {
		return err
	}
	if len(before) > 1024*1024 {
		return errors.New("fstab file exceeds safe size limit")
	}
	after, changed, err := Merge(before, e)
	if err != nil || !changed {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if _, err := file.Write(after); err != nil {
		return err
	}
	return file.Sync()
}
