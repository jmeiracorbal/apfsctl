package policy

import (
	"context"
	"errors"
	"fmt"
)

type Desired struct {
	VolumeUUID      string
	MountPoint      string
	OwnerUID        int // -1 leaves ownership unchanged
	SpotlightOff    bool
	DisableFSEvents bool // default false preserves FSEvents state
}

type Platform interface {
	EnsureFstab(context.Context, string, string) (bool, error)
	SpotlightEnabled(context.Context, string) (bool, error)
	DisableSpotlight(context.Context, string) error
	FSEventsDisabled(string) (bool, error)
	DisableFSEvents(string) error
	OwnerUID(string) (int, error)
	ChangeOwner(string, int) error
}

type Locker interface{ Acquire() (func() error, error) }

type Service struct {
	Platform    Platform
	Lock        Locker
	VerifyMount func(context.Context) error
}

type Result struct {
	Noop    bool     `json:"noop"`
	Applied []string `json:"applied"`
}

// Apply reads actual policy state before every write and rechecks APFS identity.
func (s Service) Apply(ctx context.Context, d Desired) (result Result, err error) {
	result = Result{Noop: true, Applied: []string{}}
	if s.Platform == nil || s.Lock == nil || s.VerifyMount == nil {
		return result, errors.New("missing policy dependency")
	}
	if d.OwnerUID < -1 { return result, errors.New("invalid owner UID") }
	release, err := s.Lock.Acquire()
	if err != nil { return result, err }
	defer func() {
		if e := release(); e != nil && err == nil { err = e }
	}()
	verify := func() error { return s.VerifyMount(ctx) }
	if err = verify(); err != nil { return result, err }
	changed, err := s.Platform.EnsureFstab(ctx, d.VolumeUUID, d.MountPoint)
	if err != nil { return result, err }
	if changed {
		result.Noop = false
		result.Applied = append(result.Applied, "fstab")
	}
	if d.SpotlightOff {
		if err = verify(); err != nil { return result, err }
		enabled, e := s.Platform.SpotlightEnabled(ctx, d.MountPoint)
		if e != nil { return result, e }
		if enabled {
			if err = s.Platform.DisableSpotlight(ctx, d.MountPoint); err != nil { return result, err }
			enabled, err = s.Platform.SpotlightEnabled(ctx, d.MountPoint)
			if err != nil { return result, err }
			if enabled { return result, errors.New("Spotlight remains enabled after update") }
			result.Noop = false
			result.Applied = append(result.Applied, "spotlight")
		}
	}
	if d.DisableFSEvents {
		if err = verify(); err != nil { return result, err }
		disabled, e := s.Platform.FSEventsDisabled(d.MountPoint)
		if e != nil { return result, e }
		if !disabled {
			if err = s.Platform.DisableFSEvents(d.MountPoint); err != nil { return result, err }
			disabled, err = s.Platform.FSEventsDisabled(d.MountPoint)
			if err != nil { return result, err }
			if !disabled { return result, errors.New("FSEvents marker absent after update") }
			result.Noop = false
			result.Applied = append(result.Applied, "fsevents")
		}
	}
	if d.OwnerUID >= 0 {
		if err = verify(); err != nil { return result, err }
		uid, e := s.Platform.OwnerUID(d.MountPoint)
		if e != nil { return result, e }
		if uid != d.OwnerUID {
			if err = s.Platform.ChangeOwner(d.MountPoint, d.OwnerUID); err != nil { return result, err }
			uid, err = s.Platform.OwnerUID(d.MountPoint)
			if err != nil { return result, err }
			if uid != d.OwnerUID { return result, fmt.Errorf("owner UID still %d after change", uid) }
			result.Noop = false
			result.Applied = append(result.Applied, "owner")
		}
	}
	return result, nil
}
