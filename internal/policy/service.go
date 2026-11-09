package policy

import (
	"context"
	"errors"
	"fmt"
)

type Desired struct {
	VolumeUUID string
	MountPoint string
	Browsable  bool
	OwnerUID   int    // -1 leaves ownership unchanged
	Spotlight  string // keep, on, or off
	FSEvents   string // keep, enable, or disable
}

type Platform interface {
	EnsureFstab(context.Context, string, string, bool) (bool, error)
	SpotlightEnabled(context.Context, string) (bool, error)
	DisableSpotlight(context.Context, string) error
	EnableSpotlight(context.Context, string) error
	FSEventsDisabled(string) (bool, error)
	DisableFSEvents(string) error
	EnableFSEvents(string) error
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
	if d.OwnerUID < -1 {
		return result, errors.New("invalid owner UID")
	}
	if d.Spotlight != "keep" && d.Spotlight != "on" && d.Spotlight != "off" {
		return result, errors.New("Spotlight setting must be keep, on, or off")
	}
	if d.FSEvents != "keep" && d.FSEvents != "enable" && d.FSEvents != "disable" {
		return result, errors.New("FSEvents setting must be keep, enable, or disable")
	}
	release, err := s.Lock.Acquire()
	if err != nil {
		return result, err
	}
	defer func() {
		if e := release(); e != nil && err == nil {
			err = e
		}
	}()
	verify := func() error { return s.VerifyMount(ctx) }
	if err = verify(); err != nil {
		return result, err
	}
	changed, err := s.Platform.EnsureFstab(ctx, d.VolumeUUID, d.MountPoint, d.Browsable)
	if err != nil {
		return result, err
	}
	if changed {
		result.Noop = false
		result.Applied = append(result.Applied, "fstab")
	}
	if d.Spotlight != "keep" {
		if err = verify(); err != nil {
			return result, err
		}
		enabled, e := s.Platform.SpotlightEnabled(ctx, d.MountPoint)
		if e != nil {
			return result, e
		}
		wantEnabled := d.Spotlight == "on"
		if enabled != wantEnabled {
			if wantEnabled {
				err = s.Platform.EnableSpotlight(ctx, d.MountPoint)
			} else {
				err = s.Platform.DisableSpotlight(ctx, d.MountPoint)
			}
			if err != nil {
				return result, err
			}
			updated, verifyErr := s.Platform.SpotlightEnabled(ctx, d.MountPoint)
			err = verifyErr
			if err != nil {
				return result, err
			}
			if updated != wantEnabled {
				return result, fmt.Errorf("Spotlight state did not become %q", d.Spotlight)
			}
			result.Noop = false
			result.Applied = append(result.Applied, "spotlight_"+d.Spotlight)
		}
	}
	if d.FSEvents != "keep" {
		if err = verify(); err != nil {
			return result, err
		}
		disabled, e := s.Platform.FSEventsDisabled(d.MountPoint)
		if e != nil {
			return result, e
		}
		wantDisabled := d.FSEvents == "disable"
		if disabled != wantDisabled {
			if wantDisabled {
				err = s.Platform.DisableFSEvents(d.MountPoint)
			} else {
				err = s.Platform.EnableFSEvents(d.MountPoint)
			}
			if err != nil {
				return result, err
			}
			updated, verifyErr := s.Platform.FSEventsDisabled(d.MountPoint)
			err = verifyErr
			if err != nil {
				return result, err
			}
			if updated != wantDisabled {
				return result, fmt.Errorf("FSEvents state did not become %q", d.FSEvents)
			}
			result.Noop = false
			result.Applied = append(result.Applied, "fsevents_"+d.FSEvents)
		}
	}
	if d.OwnerUID >= 0 {
		if err = verify(); err != nil {
			return result, err
		}
		uid, e := s.Platform.OwnerUID(d.MountPoint)
		if e != nil {
			return result, e
		}
		if uid != d.OwnerUID {
			if err = s.Platform.ChangeOwner(d.MountPoint, d.OwnerUID); err != nil {
				return result, err
			}
			uid, err = s.Platform.OwnerUID(d.MountPoint)
			if err != nil {
				return result, err
			}
			if uid != d.OwnerUID {
				return result, fmt.Errorf("owner UID still %d after change", uid)
			}
			result.Noop = false
			result.Applied = append(result.Applied, "owner")
		}
	}
	return result, nil
}
