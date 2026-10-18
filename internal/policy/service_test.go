package policy

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakePlatform struct {
	fstab bool
	spotlight bool
	events bool
	owner int
	writes []string
}

func (f *fakePlatform) EnsureFstab(_ context.Context, _, _ string) (bool, error) {
	if f.fstab { return false, nil }
	f.fstab = true
	f.writes = append(f.writes, "fstab")
	return true, nil
}
func (f *fakePlatform) SpotlightEnabled(_ context.Context, _ string) (bool, error) {
	return f.spotlight, nil
}
func (f *fakePlatform) DisableSpotlight(_ context.Context, _ string) error {
	f.spotlight = false
	f.writes = append(f.writes, "spotlight")
	return nil
}
func (f *fakePlatform) FSEventsDisabled(_ string) (bool, error) { return f.events, nil }
func (f *fakePlatform) DisableFSEvents(_ string) error {
	f.events = true
	f.writes = append(f.writes, "fsevents")
	return nil
}
func (f *fakePlatform) OwnerUID(_ string) (int, error) { return f.owner, nil }
func (f *fakePlatform) ChangeOwner(_ string, uid int) error {
	f.owner = uid
	f.writes = append(f.writes, "owner")
	return nil
}

type fakeLock struct{ n int }
func (l *fakeLock) Acquire() (func() error, error) {
	l.n++
	return func() error { return nil }, nil
}

func TestApplyIdempotentAndPreservesFSEvents(t *testing.T) {
	p := &fakePlatform{spotlight: true, owner: 0}
	lock := &fakeLock{}
	svc := Service{Platform: p, Lock: lock, VerifyMount: func(context.Context) error { return nil }}
	d := Desired{VolumeUUID: "A", MountPoint: "/test", OwnerUID: 501, SpotlightOff: true}
	got, err := svc.Apply(context.Background(), d)
	if err != nil { t.Fatal(err) }
	if got.Noop || !reflect.DeepEqual(p.writes, []string{"fstab", "spotlight", "owner"}) || p.events {
		t.Fatalf("unexpected mutations: %+v", p)
	}
	got, err = svc.Apply(context.Background(), d)
	if err != nil || !got.Noop || len(got.Applied) != 0 || len(p.writes) != 3 {
		t.Fatalf("second apply must not mutate: %+v %v", got, err)
	}
}

func TestFailClosedMountVerification(t *testing.T) {
	p := &fakePlatform{}
	svc := Service{Platform: p, Lock: &fakeLock{}, VerifyMount: func(context.Context) error {
		return errors.New("unmounted")
	}}
	if _, err := svc.Apply(context.Background(), Desired{OwnerUID: -1}); err == nil || len(p.writes) != 0 {
		t.Fatal("must fail before writing")
	}
}

func TestOptionalDisableEvents(t *testing.T) {
	p := &fakePlatform{fstab: true}
	svc := Service{Platform: p, Lock: &fakeLock{}, VerifyMount: func(context.Context) error { return nil }}
	_, err := svc.Apply(context.Background(), Desired{DisableFSEvents: true, OwnerUID: -1})
	if err != nil || !p.events { t.Fatalf("marker should be added: %v", err) }
	_, err = svc.Apply(context.Background(), Desired{DisableFSEvents: true, OwnerUID: -1})
	if err != nil || len(p.writes) != 1 { t.Fatalf("must not rewrite marker: %v", err) }
}
