package projectmove

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/managed"
)

type Copier interface {
	Copy(context.Context, string, string) error
}

type Ditto struct{}

func (Ditto) Copy(ctx context.Context, source, dest string) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/ditto", "--rsrc", "--extattr", "--acl", source, dest)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("copy project using ditto: %w", err)
	}
	return nil
}

type Service struct {
	Snapshot func(context.Context) (domain.Snapshot, error)
	Copy Copier
}

type Result struct {
	Source string `json:"source"`
	Destination string `json:"destination"`
	Volume string `json:"volume"`
	Noop bool `json:"noop"`
}

// Move does not require administrator privileges. The current user must have
// write access to the mounted destination volume.
func (s Service) Move(ctx context.Context, source, targetName string, records []managed.Record) (Result, error) {
	if s.Snapshot == nil || s.Copy == nil {
		return Result{}, errors.New("move service dependencies are missing")
	}
	if source == "" || targetName == "" {
		return Result{}, errors.New("move requires a source directory and a managed volume name")
	}
	path, err := filepath.Abs(source)
	if err != nil {
		return Result{}, err
	}
	path = filepath.Clean(path)
	if path == "/" {
		return Result{}, errors.New("refusing to move the filesystem root")
	}
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return Result{}, err
	}
	available, err := managed.Available(records, snapshot)
	if err != nil {
		return Result{}, err
	}
	var matches []domain.Volume
	for _, volume := range available {
		if volume.Name == targetName {
			matches = append(matches, volume)
		}
	}
	if len(matches) != 1 {
		return Result{}, fmt.Errorf("managed and mounted APFS volume %q is not uniquely available", targetName)
	}
	volume := matches[0]
	mountPoint, err := filepath.EvalSymlinks(volume.MountPoint)
	if err != nil {
		return Result{}, err
	}
	if mountPoint == "/" {
		return Result{}, errors.New("refusing root volume as destination")
	}
	if st, err := os.Lstat(mountPoint); err != nil || !st.IsDir() {
		return Result{}, errors.New("managed volume mount point is not an accessible directory")
	}
	name := filepath.Base(path)
	destination := filepath.Join(mountPoint, name)
	result := Result{Source: path, Destination: destination, Volume: volume.Name}

	verifyVolume := func() error {
		current, err := s.Snapshot(ctx)
		if err != nil {
			return err
		}
		active, err := managed.Available(records, current)
		if err != nil {
			return err
		}
		for _, v := range active {
			if strings.EqualFold(v.UUID, volume.UUID) && v.Name == volume.Name {
				actual, err := filepath.EvalSymlinks(v.MountPoint)
				if err != nil {
					return err
				}
				if actual == mountPoint {
					return nil
				}
			}
		}
		return errors.New("managed destination volume changed during move")
	}
	if err := verifyVolume(); err != nil {
		return result, err
	}
	record, err := readRecord(path, volume.UUID)
	if err != nil {
		return result, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if record == nil || record.Destination != destination {
			return result, fmt.Errorf("project source %q does not exist", path)
		}
		actual, err := manifest(destination)
		if err != nil { return result, err }
		fingerprint, err := digest(actual)
		if err != nil { return result, err }
		if fingerprint != record.Digest {
			return result, errors.New("completed transfer differs from recorded project contents")
		}
		result.Noop = true
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("project source must be a real directory")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return result, err
	}
	if canonical == mountPoint || within(mountPoint, canonical) {
		return result, errors.New("source is already inside the selected managed volume")
	}
	if within(canonical, mountPoint) {
		return result, errors.New("selected volume is nested inside the source project")
	}
	if _, err := os.Lstat(destination); err == nil {
		if record == nil || record.Destination != destination {
			return result, fmt.Errorf("destination %q already exists; refusing to overwrite", destination)
		}
		if err := verifyVolume(); err != nil {return result, err}
		sourceManifest, err := manifest(path)
		if err != nil {return result, err}
		actual, err := manifest(destination)
		if err != nil {return result, err}
		sourceHash, err := digest(sourceManifest)
		if err != nil {return result, err}
		targetHash, err := digest(actual)
		if err != nil {return result, err}
		if sourceHash != record.Digest || targetHash != record.Digest {
			return result, errors.New("previous transfer verification differs; refusing to delete source")
		}
		if err := os.RemoveAll(path); err != nil {return result, err}
		return result,nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if record != nil {
		return result, errors.New("previously completed transfer has no destination; refusing to overwrite its record")
	}
	sourceManifest, err := manifest(path)
	if err != nil {
		return result, err
	}
	// A private staging directory ensures the public target does not become
	// visible until a complete copy has been verified. It is removed on return.
	temp, err := os.MkdirTemp(mountPoint, ".apfsctl-move-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(temp)
	staged := filepath.Join(temp, name)
	if err := s.Copy.Copy(ctx, path, staged); err != nil {
		return result, err
	}
	stagedManifest, err := manifest(staged)
	if err != nil {
		return result, err
	}
	if !equal(sourceManifest, stagedManifest) {
		return result, errors.New("copied project failed content and metadata verification; source preserved")
	}
	latest, err := manifest(path)
	if err != nil {
		return result, err
	}
	if !equal(sourceManifest, latest) {
		return result, errors.New("project changed during copy; source preserved")
	}
	if err := verifyVolume(); err != nil {
		return result, err
	}
	if _, err := os.Lstat(destination); err == nil {
		return result, errors.New("destination appeared during copy; source preserved")
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err := os.Rename(staged, destination); err != nil {
		return result, fmt.Errorf("publish verified project: %w", err)
	}
	// Never delete the source unless the published directory also verifies.
	published, err := manifest(destination)
	if err != nil || !equal(sourceManifest, published) {
		return result, errors.New("destination verification after rename failed; source preserved")
	}
	fingerprint, err := digest(sourceManifest)
	if err != nil {return result, err}
	if err := writeRecord(moveRecord{
		Source: path, Destination: destination, VolumeUUID: volume.UUID, Digest: fingerprint,
	}); err != nil {
		return result, fmt.Errorf("verified copy preserved but move journal failed: %w",err)
	}
	if err := os.RemoveAll(path); err != nil {
		return result, fmt.Errorf("verified destination exists but source cleanup failed: %w", err)
	}
	return result, nil
}

func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

type entry struct {
	Path string
	Kind os.FileMode
	Mode os.FileMode
	Link string
	Hash string
}

func manifest(root string) ([]entry, error) {
	items := []entry{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		e := entry{Path: rel, Kind: info.Mode().Type(), Mode: info.Mode().Perm()}
		switch {
		case info.Mode().IsRegular():
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, err = io.Copy(hash, f)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			e.Hash = hex.EncodeToString(hash.Sum(nil))
		case info.IsDir():
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			e.Link = link
		default:
			return fmt.Errorf("unsupported special filesystem object: %s", path)
		}
		items = append(items, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}
func equal(a,b []entry) bool {
	if len(a)!=len(b) { return false }
	for i := range a {
		if a[i]!=b[i] { return false }
	}
	return true
}
