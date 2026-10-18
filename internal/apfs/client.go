package apfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmeiracorbal/verbose-octo-spork/internal/domain"
	"github.com/jmeiracorbal/verbose-octo-spork/internal/system"
)

const (
	diskutil = "/usr/sbin/diskutil"
	plutil   = "/usr/bin/plutil"
)

type Client struct{ Run system.Runner }

// Snapshot always reads the live state. No local marker can substitute for
// the observed APFS UUID, encryption state or active mount point.
func (c Client) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	if c.Run == nil {
		return domain.Snapshot{}, errors.New("missing command runner")
	}
	var listing struct {
		Containers []struct {
			UUID      string `json:"APFSContainerUUID"`
			Reference string `json:"ContainerReference"`
			FreeBytes uint64 `json:"CapacityFree"`
			Volumes   []struct {
				Name       string   `json:"Name"`
				UUID       string   `json:"APFSVolumeUUID"`
				Device     string   `json:"DeviceIdentifier"`
				Encryption *bool    `json:"Encryption"`
				Locked     *bool    `json:"Locked"`
				Roles      []string `json:"Roles"`
			} `json:"Volumes"`
		} `json:"Containers"`
	}
	if err := c.readPlist(ctx, &listing, diskutil, "apfs", "list", "-plist"); err != nil {
		return domain.Snapshot{}, err
	}
	if len(listing.Containers) == 0 {
		return domain.Snapshot{}, errors.New("no APFS containers discovered")
	}
	s := domain.Snapshot{Containers: []domain.Container{}, Volumes: []domain.Volume{}}
	for _, container := range listing.Containers {
		s.Containers = append(s.Containers, domain.Container{
			UUID: container.UUID, Reference: container.Reference, FreeBytes: container.FreeBytes,
		})
		for _, v := range container.Volumes {
			if v.Encryption == nil || v.Locked == nil {
				return domain.Snapshot{}, fmt.Errorf("encryption/lock state unavailable for volume %q", v.Name)
			}
			var info struct {
				MountPoint string `json:"MountPoint"`
			}
			if err := c.readPlist(ctx, &info, diskutil, "info", "-plist", v.Device); err != nil {
				return domain.Snapshot{}, fmt.Errorf("inspect %s: %w", v.Device, err)
			}
			s.Volumes = append(s.Volumes, domain.Volume{
				Name: v.Name, UUID: v.UUID, ContainerUUID: container.UUID,
				Device: v.Device, Encrypted: *v.Encryption, Locked: *v.Locked,
				MountPoint: info.MountPoint, Roles: v.Roles,
			})
		}
	}
	if err := s.Validate(); err != nil {
		return domain.Snapshot{}, err
	}
	return s, nil
}

func (c Client) readPlist(ctx context.Context, out any, executable string, args ...string) error {
	plist, err := c.Run.Run(ctx, executable, nil, args...)
	if err != nil {
		return fmt.Errorf("read APFS plist: %w", err)
	}
	if len(plist) == 0 {
		return errors.New("empty APFS plist response")
	}
	data, err := c.Run.Run(ctx, plutil, plist, "-convert", "json", "-o", "-", "-")
	if err != nil {
		return fmt.Errorf("convert APFS plist: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode APFS JSON: %w", err)
	}
	return nil
}
