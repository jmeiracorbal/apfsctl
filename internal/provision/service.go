package provision

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/reconcile"
	"github.com/jmeiracorbal/apfsctl/internal/state"
)

type Platform interface {
	Snapshot(context.Context) (domain.Snapshot, error)
	CreateEncrypted(context.Context, domain.Container, string) error
	Unlock(context.Context, domain.Volume) error
	Mount(context.Context, domain.Volume, string, bool) error
	Unmount(context.Context, domain.Volume) error
}

type Journal interface {
	Load(domain.Desired) (*state.Entry, error)
	Save(state.Entry) error
}

type Locker interface {
	Acquire() (func() error, error)
}

type Service struct {
	Platform Platform
	Journal  Journal
	Lock     Locker
	// ValidateMount must reject absent, nonempty, symlinked or already mounted paths.
	ValidateMount func(string) error
}

type Result struct {
	Noop       bool     `json:"noop"`
	VolumeUUID string   `json:"volume_uuid"`
	Applied    []string `json:"applied"`
}

// Apply reconciles the observed state under a host-wide lock. Each operation
// is followed by a fresh snapshot; a journal alone never determines success.
// When allowCreate is false, no new volume may be provisioned.
func (s Service) Apply(ctx context.Context, desired domain.Desired, allowCreate bool) (result Result, err error) {
	result.Applied = []string{}
	if err := desired.Validate(); err != nil {
		return result, err
	}
	if s.Platform == nil || s.Journal == nil || s.Lock == nil || s.ValidateMount == nil {
		return result, errors.New("provisioner requires platform, journal, lock and mount validator")
	}

	first, err := s.Platform.Snapshot(ctx)
	if err != nil {
		return result, err
	}
	p, err := reconcile.Build(desired, first)
	if err != nil {
		return result, err
	}
	if !allowCreate && hasAction(p, reconcile.CreateEncrypted) {
		return result, errors.New("volume does not exist; mount refuses to create it")
	}
	if p.Noop {
		result.Noop = true
		result.VolumeUUID = findUUID(first, desired)
		return result, nil
	}
	if firstAction(p) != reconcile.Unmount {
		if err := s.ValidateMount(desired.MountPoint); err != nil {
			return result, err
		}
	}
	release, err := s.Lock.Acquire()
	if err != nil {
		return result, err
	}
	defer func() {
		if e := release(); e != nil && err == nil {
			err = fmt.Errorf("release operation lock: %w", e)
		}
	}()

	// Re-read after acquiring the lock: the first snapshot may now be stale.
	entry, err := s.Journal.Load(desired)
	if err != nil {
		return result, err
	}
	if entry == nil {
		entry = &state.Entry{Desired: desired, Phase: "pending"}
	}
	for step := 0; step < 5; step++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		snap, err := s.Platform.Snapshot(ctx)
		if err != nil {
			return result, err
		}
		if entry.VolumeUUID != "" {
			uuid := findUUID(snap, desired)
			if uuid != "" && !strings.EqualFold(uuid, entry.VolumeUUID) {
				return result, errors.New("observed APFS volume UUID differs from operation journal")
			}
		}
		plan, err := reconcile.Build(desired, snap)
		if err != nil {
			return result, err
		}
		if !allowCreate && hasAction(plan, reconcile.CreateEncrypted) {
			return result, errors.New("volume disappeared; refusing creation")
		}
		if plan.Noop {
			result.Noop = len(result.Applied) == 0
			result.VolumeUUID = findUUID(snap, desired)
			if entry.Phase != "completed" || entry.VolumeUUID != result.VolumeUUID {
				entry.Phase = "completed"
				entry.VolumeUUID = result.VolumeUUID
				if err := s.Journal.Save(*entry); err != nil {
					return result, err
				}
			}
			return result, nil
		}
		if len(plan.Changes) == 0 {
			return result, errors.New("non-converged plan has no changes")
		}
		action := plan.Changes[0].Action
		if action == reconcile.CreateEncrypted || action == reconcile.Mount {
			if err := s.ValidateMount(desired.MountPoint); err != nil {
				return result, err
			}
		}
		switch action {
		case reconcile.CreateEncrypted:
			var container *domain.Container
			for i := range snap.Containers {
				if strings.EqualFold(snap.Containers[i].UUID, desired.ContainerUUID) {
					container = &snap.Containers[i]
					break
				}
			}
			if container == nil {
				return result, errors.New("container disappeared during reconciliation")
			}
			entry.Phase = "creating"
			if err := s.Journal.Save(*entry); err != nil {
				return result, err
			}
			if err := s.Platform.CreateEncrypted(ctx, *container, desired.Name); err != nil {
				return result, fmt.Errorf("creation may have partially completed; rerun after checking status: %w", err)
			}
		case reconcile.Unlock:
			v, ok := findVolume(snap, desired)
			if !ok {
				return result, errors.New("volume disappeared before unlock")
			}
			entry.Phase = "unlocking"
			if err := s.Journal.Save(*entry); err != nil {
				return result, err
			}
			if err := s.Platform.Unlock(ctx, v); err != nil {
				return result, err
			}
		case reconcile.Unmount:
			v, ok := findVolume(snap, desired)
			if !ok || v.MountPoint != desired.MountPoint {
				return result, errors.New("volume changed before unmount")
			}
			entry.Phase = "unmounting"
			if err := s.Journal.Save(*entry); err != nil {
				return result, err
			}
			if err := s.Platform.Unmount(ctx, v); err != nil {
				return result, err
			}
		case reconcile.Mount:
			v, ok := findVolume(snap, desired)
			if !ok {
				return result, errors.New("volume disappeared before mount")
			}
			entry.Phase = "mounting"
			if err := s.Journal.Save(*entry); err != nil {
				return result, err
			}
			if err := s.Platform.Mount(ctx, v, desired.MountPoint, desired.Browsable); err != nil {
				return result, err
			}
		default:
			return result, fmt.Errorf("unsupported reconciliation action %q", action)
		}
		next, err := s.Platform.Snapshot(ctx)
		if err != nil {
			return result, fmt.Errorf("operation executed; cannot verify actual APFS state: %w", err)
		}
		check, err := reconcile.Build(desired, next)
		if err != nil {
			return result, fmt.Errorf("operation executed; observed APFS conflict: %w", err)
		}
		if hasAction(check, action) {
			return result, fmt.Errorf("%s did not change observed APFS state", action)
		}
		entry.VolumeUUID = findUUID(next, desired)
		entry.Phase = string(action) + "_verified"
		if err := s.Journal.Save(*entry); err != nil {
			return result, err
		}
		result.Applied = append(result.Applied, string(action))
	}
	return result, errors.New("APFS reconciliation exceeded maximum safe operations")
}

func firstAction(p reconcile.Plan) reconcile.Action {
	if len(p.Changes) == 0 {
		return ""
	}
	return p.Changes[0].Action
}

func hasAction(p reconcile.Plan, action reconcile.Action) bool {
	for _, c := range p.Changes {
		if c.Action == action {
			return true
		}
	}
	return false
}

func findVolume(s domain.Snapshot, d domain.Desired) (domain.Volume, bool) {
	for _, v := range s.Volumes {
		if v.Name == d.Name && strings.EqualFold(v.ContainerUUID, d.ContainerUUID) {
			return v, true
		}
	}
	return domain.Volume{}, false
}

func findUUID(s domain.Snapshot, d domain.Desired) string {
	if v, found := findVolume(s, d); found {
		return v.UUID
	}
	return ""
}
