package domain

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var deviceID = regexp.MustCompile("^disk[0-9]+(s[0-9]+)?$")

type Desired struct {
	Name          string `json:"name"`
	ContainerUUID string `json:"container_uuid"`
	MountPoint    string `json:"mount_point"`
	Encrypted     bool   `json:"encrypted"`
}

func (d Desired) Validate() error {
	if d.Name == "" || strings.TrimSpace(d.Name) != d.Name ||
		strings.ContainsAny(d.Name, "/:\x00\r\n") || strings.HasPrefix(d.Name, "-") {
		return errors.New("volume name is empty or contains unsupported characters")
	}
	if strings.TrimSpace(d.ContainerUUID) == "" ||
		strings.TrimSpace(d.ContainerUUID) != d.ContainerUUID {
		return errors.New("container UUID is required")
	}
	if !filepath.IsAbs(d.MountPoint) ||
		filepath.Clean(d.MountPoint) != d.MountPoint || d.MountPoint == "/" {
		return errors.New("mount point must be a clean absolute path other than /")
	}
	if !d.Encrypted {
		return errors.New("unencrypted volumes are not supported")
	}
	return nil
}

// Container identifies the APFS device by its current reference, never by
// guessing diskN from a volume device. The UUID is its stable identity.
type Container struct {
	UUID      string `json:"uuid"`
	Reference string `json:"reference"`
	FreeBytes uint64 `json:"free_bytes"`
}

type Volume struct {
	Name          string   `json:"name"`
	UUID          string   `json:"uuid"`
	ContainerUUID string   `json:"container_uuid"`
	Device        string   `json:"device"`
	Encrypted     bool     `json:"encrypted"`
	Locked        bool     `json:"locked"`
	MountPoint    string   `json:"mount_point,omitempty"`
	Roles         []string `json:"roles,omitempty"`
}

type Snapshot struct {
	Containers []Container `json:"containers"`
	Volumes    []Volume    `json:"volumes"`
}

// Validate fails closed. Unknown or inconsistent device state must never be
// interpreted as permission to create a new volume.
func (s Snapshot) Validate() error {
	containers := make(map[string]struct{}, len(s.Containers))
	references := make(map[string]struct{}, len(s.Containers))
	for _, c := range s.Containers {
		if c.UUID == "" || !deviceID.MatchString(c.Reference) {
			return errors.New("invalid APFS container identity")
		}
		key := strings.ToUpper(c.UUID)
		if _, exists := containers[key]; exists {
			return fmt.Errorf("duplicate APFS container UUID %q", c.UUID)
		}
		if _, exists := references[c.Reference]; exists {
			return fmt.Errorf("duplicate APFS container reference %q", c.Reference)
		}
		containers[key] = struct{}{}
		references[c.Reference] = struct{}{}
	}
	uuids := make(map[string]struct{}, len(s.Volumes))
	devices := make(map[string]struct{}, len(s.Volumes))
	mounts := make(map[string]struct{}, len(s.Volumes))
	for _, v := range s.Volumes {
		if v.UUID == "" || v.Name == "" || !deviceID.MatchString(v.Device) {
			return errors.New("incomplete APFS volume observation")
		}
		if _, exists := containers[strings.ToUpper(v.ContainerUUID)]; !exists {
			return fmt.Errorf("APFS volume %q refers to an unknown container", v.Name)
		}
		id := strings.ToUpper(v.UUID)
		if _, exists := uuids[id]; exists {
			return fmt.Errorf("duplicate APFS volume UUID %q", v.UUID)
		}
		if _, exists := devices[v.Device]; exists {
			return fmt.Errorf("duplicate APFS device %q", v.Device)
		}
		uuids[id] = struct{}{}
		devices[v.Device] = struct{}{}
		if v.MountPoint != "" {
			if !filepath.IsAbs(v.MountPoint) || filepath.Clean(v.MountPoint) != v.MountPoint {
				return fmt.Errorf("invalid mount point for volume %q", v.Name)
			}
			if _, exists := mounts[v.MountPoint]; exists {
				return fmt.Errorf("duplicate APFS mount point %q", v.MountPoint)
			}
			if v.Locked {
				return fmt.Errorf("volume %q is reported as locked and mounted", v.Name)
			}
			mounts[v.MountPoint] = struct{}{}
		}
	}
	return nil
}
