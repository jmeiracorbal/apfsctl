package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/state"
)

const (
	RegistryPath = "/Library/Application Support/apfsctl/volumes.json"
	JournalDir   = "/private/var/db/apfsctl"
	LockPath     = "/private/var/db/apfsctl/operation.lock"
)

type Record struct {
	Name          string `json:"name"`
	VolumeUUID    string `json:"volume_uuid"`
	ContainerUUID string `json:"container_uuid"`
}

type Store struct{ Path string }

func (s Store) Read() ([]Record, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	var records []Record
	if err := json.Unmarshal(b, &records); err != nil {
		return nil, fmt.Errorf("read managed volume registry: %w", err)
	}
	seen := map[string]bool{}
	for _, r := range records {
		if r.Name == "" || r.VolumeUUID == "" || r.ContainerUUID == "" || seen[strings.ToUpper(r.VolumeUUID)] {
			return nil, errors.New("invalid or duplicate managed volume record")
		}
		seen[strings.ToUpper(r.VolumeUUID)] = true
	}
	return records, nil
}

// Available accepts only volumes recorded by apfsctl and mounted in a live
// APFS snapshot. Mount paths may be anywhere, not just under /Volumes.
func Available(records []Record, snapshot domain.Snapshot) ([]domain.Volume, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	volumes := []domain.Volume{}
	for _, record := range records {
		for _, v := range snapshot.Volumes {
			if !strings.EqualFold(v.UUID, record.VolumeUUID) {
				continue
			}
			if v.Name != record.Name || !strings.EqualFold(v.ContainerUUID, record.ContainerUUID) {
				return nil, fmt.Errorf("managed volume %q changed identity", record.Name)
			}
			if v.Encrypted && !v.Locked && v.MountPoint != "" && len(v.Roles) == 0 {
				volumes = append(volumes, v)
			}
		}
	}
	sort.Slice(volumes, func(i, j int) bool { return volumes[i].Name < volumes[j].Name })
	return volumes, nil
}

// Refresh publishes only confirmed completed journal entries. It is run by
// an administrator; the resulting registry contains public volume IDs only.
func Refresh(ctx context.Context, snapshot func(context.Context) (domain.Snapshot, error)) error {
	if os.Geteuid() != 0 {
		return errors.New("refreshing managed volumes requires administrator privileges")
	}
	release, err := (state.FileLock{Path: LockPath}).Acquire()
	if err != nil {
		return err
	}
	defer release()
	snap, err := snapshot(ctx)
	if err != nil {
		return err
	}
	if err := snap.Validate(); err != nil {
		return err
	}
	files, err := os.ReadDir(JournalDir)
	if err != nil {
		return err
	}
	records := []Record{}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		path := filepath.Join(JournalDir, file.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || st.Uid != 0 || info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("unsafe APFS journal file %q", path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var entry state.Entry
		if err := json.Unmarshal(b, &entry); err != nil {
			return fmt.Errorf("invalid APFS journal %q: %w", path, err)
		}
		if entry.Phase != "completed" || entry.VolumeUUID == "" {
			continue
		}
		if err := entry.Desired.Validate(); err != nil {
			return err
		}
		for _, v := range snap.Volumes {
			if strings.EqualFold(v.UUID, entry.VolumeUUID) &&
				v.Name == entry.Desired.Name &&
				strings.EqualFold(v.ContainerUUID, entry.Desired.ContainerUUID) &&
				v.Encrypted && len(v.Roles) == 0 {
				records = append(records, Record{Name: v.Name, VolumeUUID: v.UUID, ContainerUUID: v.ContainerUUID})
			}
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].VolumeUUID < records[j].VolumeUUID })
	unique := records[:0]
	for _, r := range records {
		if len(unique) == 0 || !strings.EqualFold(unique[len(unique)-1].VolumeUUID, r.VolumeUUID) {
			unique = append(unique, r)
		}
	}
	return (Store{Path: RegistryPath}).Write(unique)
}

// Write creates a root-owned, world-readable registry without making the
// private operation journal readable to completion subprocesses.
func (s Store) Write(records []Record) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		st.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0022 != 0 {
		return errors.New("unsafe managed registry directory")
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, byte(10))
	old, err := os.ReadFile(s.Path)
	if err == nil && string(old) == string(data) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".volumes-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0644); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}
