package apfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jmeiracorbal/verbose-octo-spork/internal/domain"
	"github.com/jmeiracorbal/verbose-octo-spork/internal/system"
)

const (
	diskutil = "/usr/sbin/diskutil"
	plutil   = "/usr/bin/plutil"
)

type Client struct{ Run system.Runner }

// Snapshot reads structured macOS information; failure to read any part is fatal.
func (c Client) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	if c.Run == nil {
		return domain.Snapshot{}, errors.New("missing command runner")
	}
	var listing struct {
		Containers []struct {
			UUID    string `json:"APFSContainerUUID"`
			Volumes []struct {
				Name       string `json:"Name"`
				UUID       string `json:"APFSVolumeUUID"`
				Device     string `json:"DeviceIdentifier"`
				Encryption *bool  `json:"Encryption"`
				FileVault  *bool  `json:"FileVault"`
				Locked     *bool  `json:"Locked"`
			} `json:"Volumes"`
		} `json:"Containers"`
	}
	if err := c.readPlist(ctx, &listing, diskutil, "apfs", "list", "-plist"); err != nil {
		return domain.Snapshot{}, err
	}
	if len(listing.Containers) == 0 {
		return domain.Snapshot{}, errors.New("no APFS containers discovered")
	}
	s := domain.Snapshot{Containers: []string{}, Volumes: []domain.Volume{}}
	for _, container := range listing.Containers {
		if container.UUID == "" {
			return domain.Snapshot{}, errors.New("APFS container without UUID")
		}
		s.Containers = append(s.Containers, container.UUID)
		for _, v := range container.Volumes {
			if v.Encryption == nil || v.FileVault == nil || v.Locked == nil {
				return domain.Snapshot{}, fmt.Errorf("encryption state unavailable for volume %q", v.Name)
			}
			if !strings.HasPrefix(v.Device, "disk") {
				return domain.Snapshot{}, fmt.Errorf("invalid APFS device identifier %q", v.Device)
			}
			var info struct {
				MountPoint string `json:"MountPoint"`
			}
			if err := c.readPlist(ctx, &info, diskutil, "info", "-plist", v.Device); err != nil {
				return domain.Snapshot{}, fmt.Errorf("inspect %s: %w", v.Device, err)
			}
			s.Volumes = append(s.Volumes, domain.Volume{
				Name: v.Name, UUID: v.UUID, ContainerUUID: container.UUID,
				Device: v.Device, Encrypted: *v.Encryption,
				Locked: *v.Locked, MountPoint: info.MountPoint,
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
	data, err := c.Run.Run(ctx, plutil, plist, "-convert", "json", "-o", "-", "-")
	if err != nil {
		return fmt.Errorf("convert APFS plist: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode APFS JSON: %w", err)
	}
	return nil
}
