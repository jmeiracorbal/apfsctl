package reconcile

import (
	"fmt"
	"strings"

	"github.com/jmeiracorbal/verbose-octo-spork/internal/domain"
)

type Action string

const (
	CreateEncrypted Action = "create_encrypted"
	Unlock          Action = "unlock"
	Mount           Action = "mount"
)

type Change struct {
	Action Action `json:"action"`
	Reason string `json:"reason"`
}

type Plan struct {
	Desired domain.Desired `json:"desired"`
	Changes []Change       `json:"changes"`
	Noop    bool           `json:"noop"`
}

// Build is pure and plans only APFS encryption and mounting. It does not
// evaluate /etc/fstab, Spotlight, FSEvents or backup configuration.
func Build(d domain.Desired, s domain.Snapshot) (Plan, error) {
	if err := d.Validate(); err != nil {
		return Plan{}, err
	}
	if err := s.Validate(); err != nil {
		return Plan{}, err
	}

	containerPresent := false
	for _, c := range s.Containers {
		if strings.EqualFold(c.UUID, d.ContainerUUID) {
			containerPresent = true
			break
		}
	}
	if !containerPresent {
		return Plan{}, fmt.Errorf("APFS container %q not found", d.ContainerUUID)
	}

	var found *domain.Volume
	for i := range s.Volumes {
		v := &s.Volumes[i]
		if v.MountPoint == d.MountPoint && v.Name != d.Name {
			return Plan{}, fmt.Errorf("mount point %q is occupied by volume %q", d.MountPoint, v.Name)
		}
		if v.Name != d.Name {
			continue
		}
		if found != nil {
			return Plan{}, fmt.Errorf("multiple volumes named %q; refusing ambiguous selection", d.Name)
		}
		found = v
	}
	p := Plan{Desired: d, Changes: []Change{}}
	if found == nil {
		p.Changes = append(p.Changes,
			Change{Action: CreateEncrypted, Reason: "volume is absent"},
			Change{Action: Mount, Reason: "new volume must be mounted"})
		return p, nil
	}
	if !strings.EqualFold(found.ContainerUUID, d.ContainerUUID) {
		return Plan{}, fmt.Errorf("volume %q exists in a different APFS container", d.Name)
	}
	if len(found.Roles) > 0 {
		return Plan{}, fmt.Errorf("volume %q has reserved APFS roles %v", d.Name, found.Roles)
	}
	if !found.Encrypted {
		return Plan{}, fmt.Errorf("volume %q exists without required APFS encryption", d.Name)
	}
	if found.MountPoint != "" && found.MountPoint != d.MountPoint {
		return Plan{}, fmt.Errorf("volume %q is already mounted at %q", d.Name, found.MountPoint)
	}
	if found.MountPoint == d.MountPoint {
		p.Noop = true
		return p, nil
	}
	if found.Locked {
		p.Changes = append(p.Changes, Change{Action: Unlock, Reason: "encrypted volume is locked"})
	}
	p.Changes = append(p.Changes, Change{Action: Mount, Reason: "volume is not mounted at requested path"})
	return p, nil
}
