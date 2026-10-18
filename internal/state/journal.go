package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jmeiracorbal/verbose-octo-spork/internal/domain"
)

type Entry struct {
	Desired    domain.Desired `json:"desired"`
	Phase      string         `json:"phase"`
	VolumeUUID string         `json:"volume_uuid,omitempty"`
}

type FileJournal struct{ Directory string }

// Journal keys are tied to logical volume identity, independent of mount paths.
func (f FileJournal) path(d domain.Desired) string {
	id := sha256.Sum256([]byte(strings.ToUpper(d.ContainerUUID) + "\x00" + d.Name))
	return filepath.Join(f.Directory, hex.EncodeToString(id[:])+".json")
}

func (f FileJournal) Load(d domain.Desired) (*Entry, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(f.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("unsafe journal directory")
	}
	if os.Geteuid() == 0 {
		if err := privateDir(f.Directory); err != nil {
			return nil, err
		}
	}
	fd, err := syscall.Open(f.path(d), syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open operation journal: %w", err)
	}
	file := os.NewFile(uintptr(fd), f.path(d))
	if file == nil {
		syscall.Close(fd)
		return nil, errors.New("cannot read operation journal")
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("journal must be a private root-owned regular file")
	}
	var entry Entry
	if err := json.NewDecoder(file).Decode(&entry); err != nil {
		return nil, fmt.Errorf("invalid operation journal: %w", err)
	}
	if entry.Desired != d {
		return nil, errors.New("operation journal conflicts with requested configuration")
	}
	return &entry, nil
}

func (f FileJournal) Save(entry Entry) error {
	if err := entry.Desired.Validate(); err != nil {
		return err
	}
	if err := privateDir(f.Directory); err != nil {
		return err
	}
	b, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(f.Directory, ".journal-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(b); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), f.path(entry.Desired)); err != nil {
		return err
	}
	parent, err := os.Open(f.Directory)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
