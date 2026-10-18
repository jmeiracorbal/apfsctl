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

// Build is pure. It fails closed on uncertain ownership or mismatching state.
// It covers encrypted APFS volume presence and mount state only; system policies
// (Spotlight, FSEvents, fstab) will be reconciled in subsequent milestones.
func Build(d domain.Desired, s domain.Snapshot) (Plan, error) {
	if err := d.Validate(); err != nil {
		return Plan{}, err
	}
	if err := s.Validate(); err != nil {
		return Plan{}, err
	}

	containerPresent := false
	for _, c := range s.Containers {
		if strings.EqualFold(c, d.ContainerUUID) {
			containerPresent = true
		}
	}
	if !containerPresent {
		return Plan{}, fmt.Errorf("APFS container %q not found", d.ContainerUUID)
	}

	var found *domain.Volume
	for i := range s.Volumes {
		v := &s.Volumes[i]
		if v.Name != d.Name {
			continue
		}
		if found != nil {
			return Plan{}, fmt.Errorf("multiple volumes named %q; refusing ambiguous selection", d.Name)
		}
		found = v
	}

	for _, v := range s.Volumes {
		if v.MountPoint == d.MountPoint && !(v.Name == d.Name && strings.EqualFold(v.ContainerUUID, d.ContainerUUID)) {
			if v.Name == d.Name {
				return Plan{}, fmt.Errorf("volume %q exists in a different APFS container", d.Name)
			}
			return Plan{}, fmt.Errorf("mount point %q is occupied by volume %q", d.MountPoint, v.Name)
		}
	}

	plan := Plan{Desired: d, Changes: []Change{}}
	if found == nil {
		plan.Changes = append(plan.Changes,
			Change{CreateEncrypted, "volume is absent"},
			Change{Mount, "new volume must be mounted"},
		)
		return plan, nil
	}
	if !strings.EqualFold(found.ContainerUUID, d.ContainerUUID) {
		return Plan{}, fmt.Errorf("volume %q exists in a different APFS container", d.Name)
	}
	if !found.Encrypted {
		return Plan{}, fmt.Errorf("volume %q exists without required APFS encryption", d.Name)
	}
	if found.MountPoint != "" && found.MountPoint != d.MountPoint {
		return Plan{}, fmt.Errorf("volume %q is already mounted at %q", d.Name, found.MountPoint)
	}
	if found.MountPoint == d.MountPoint {
		if found.Locked {
			return Plan{}, fmt.Errorf("inconsistent state: volume %q is mounted and locked", d.Name)
		}
		plan.Noop = true
		return plan, nil
	}
	if found.Locked {
		plan.Changes = append(plan.Changes, Change{Unlock, "encrypted volume is locked"})
	}
	plan.Changes = append(plan.Changes, Change{Mount, "volume is not mounted at requested path"})
	return plan, nil
}
