package domain

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Desired is the requested configuration. ContainerUUID is stable across disk renumbering.
type Desired struct {
	Name          string `json:"name"`
	ContainerUUID string `json:"container_uuid"`
	MountPoint    string `json:"mount_point"`
	Encrypted     bool   `json:"encrypted"`
}

func (d Desired) Validate() error {
	if d.Name == "" || strings.TrimSpace(d.Name) != d.Name || strings.ContainsAny(d.Name, "/:\x00\r\n") {
		return errors.New("volume name is empty or contains unsupported characters")
	}
	if strings.TrimSpace(d.ContainerUUID) == "" {
		return errors.New("container UUID is required")
	}
	if !filepath.IsAbs(d.MountPoint) || filepath.Clean(d.MountPoint) != d.MountPoint || d.MountPoint == "/" {
		return errors.New("mount point must be a clean absolute path other than /")
	}
	if !d.Encrypted {
		return errors.New("unencrypted volumes are not supported")
	}
	return nil
}

// Volume describes observed APFS state; it is never inferred from local state files.
type Volume struct {
	Name          string `json:"name"`
	UUID          string `json:"uuid"`
	ContainerUUID string `json:"container_uuid"`
	Device        string `json:"device"`
	Encrypted     bool   `json:"encrypted"`
	Locked        bool   `json:"locked"`
	MountPoint    string `json:"mount_point,omitempty"`
}

type Snapshot struct {
	Containers []string `json:"containers"`
	Volumes    []Volume `json:"volumes"`
}

func (s Snapshot) Validate() error {
	seen := make(map[string]struct{}, len(s.Volumes))
	for _, v := range s.Volumes {
		if v.UUID == "" || v.ContainerUUID == "" || v.Device == "" || v.Name == "" {
			return errors.New("incomplete APFS volume observation")
		}
		if _, ok := seen[strings.ToUpper(v.UUID)]; ok {
			return fmt.Errorf("duplicate APFS volume UUID: %s", v.UUID)
		}
		seen[strings.ToUpper(v.UUID)] = struct{}{}
	}
	return nil
}
