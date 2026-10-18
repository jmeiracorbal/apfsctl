package provision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/state"
)

type fakePlatform struct {
	snapshot domain.Snapshot
	actions  []string
	fail     string
}

func (f *fakePlatform) Snapshot(context.Context) (domain.Snapshot, error) {
	return f.snapshot, nil
}
func (f *fakePlatform) CreateEncrypted(_ context.Context, c domain.Container, name string) error {
	f.actions = append(f.actions, "create")
	if f.fail == "create" {
		return errors.New("interrupted creation")
	}
	f.snapshot.Volumes = append(f.snapshot.Volumes, domain.Volume{
		UUID: "NEW", Name: name, ContainerUUID: c.UUID,
		Device: "disk3s2", Encrypted: true, Locked: true,
	})
	return nil
}
func (f *fakePlatform) Unlock(_ context.Context, v domain.Volume, path string) error {
	f.actions = append(f.actions, "unlock")
	if f.fail == "unlock" {
		return errors.New("interrupted unlock")
	}
	for i := range f.snapshot.Volumes {
		if f.snapshot.Volumes[i].UUID == v.UUID {
			f.snapshot.Volumes[i].Locked = false
			f.snapshot.Volumes[i].MountPoint = path
		}
	}
	return nil
}
func (f *fakePlatform) Mount(_ context.Context, v domain.Volume, path string) error {
	f.actions = append(f.actions, "mount")
	if f.fail == "mount" {
		return errors.New("interrupted mount")
	}
	for i := range f.snapshot.Volumes {
		if f.snapshot.Volumes[i].UUID == v.UUID {
			f.snapshot.Volumes[i].MountPoint = path
		}
	}
	return nil
}

type memoryJournal struct {
	entry *state.Entry
	saves int
}

func (m *memoryJournal) Load(d domain.Desired) (*state.Entry, error) {
	if m.entry != nil && m.entry.Desired != d {
		return nil, errors.New("journal conflict")
	}
	return m.entry, nil
}
func (m *memoryJournal) Save(e state.Entry) error {
	m.entry = &e
	m.saves++
	return nil
}

type fakeLock struct{ acquisitions int }

func (l *fakeLock) Acquire() (func() error, error) {
	l.acquisitions++
	return func() error { return nil }, nil
}

func fixture() (domain.Desired, *fakePlatform, *memoryJournal, *fakeLock, Service) {
	d := domain.Desired{Name: "repos", ContainerUUID: "C", MountPoint: "/Users/test/repos", Encrypted: true}
	f := &fakePlatform{snapshot: domain.Snapshot{
		Containers: []domain.Container{{UUID: "C", Reference: "disk3", FreeBytes: 10_000_000_000}},
		Volumes:    []domain.Volume{},
	}}
	j := &memoryJournal{}
	l := &fakeLock{}
	s := Service{Platform: f, Journal: j, Lock: l, ValidateMount: func(string) error { return nil }}
	return d, f, j, l, s
}

func TestCreateThenNoop(t *testing.T) {
	d, f, j, l, s := fixture()
	out, err := s.Apply(context.Background(), d, true)
	if err != nil {
		t.Fatal(err)
	}
	if out.Noop || out.VolumeUUID != "NEW" || strings.Join(out.Applied, ",") != "create_encrypted,unlock" {
		t.Fatalf("unexpected result: %+v", out)
	}
	if j.entry.Phase != "completed" || l.acquisitions != 1 {
		t.Fatal("operation should finish with a journal and one lock")
	}
	prevSaves := j.saves
	out, err = s.Apply(context.Background(), d, true)
	if err != nil || !out.Noop || len(f.actions) != 2 || j.saves != prevSaves || l.acquisitions != 1 {
		t.Fatalf("repeat must have no side effects: result=%+v error=%v", out, err)
	}
}

func TestResumeFromCreatedLockedVolume(t *testing.T) {
	d, f, j, _, s := fixture()
	f.snapshot.Volumes = append(f.snapshot.Volumes, domain.Volume{
		Name: "repos", UUID: "ALREADY", ContainerUUID: "C",
		Device: "disk3s2", Encrypted: true, Locked: true,
	})
	j.entry = &state.Entry{Desired: d, Phase: "creating"}
	out, err := s.Apply(context.Background(), d, true)
	if err != nil || strings.Join(f.actions, ",") != "unlock" || out.VolumeUUID != "ALREADY" {
		t.Fatalf("unexpected recovery: %+v, %v, %v", out, f.actions, err)
	}
}

func TestFailedCreateDoesNotRetryWithinRun(t *testing.T) {
	d, f, j, _, s := fixture()
	f.fail = "create"
	if _, err := s.Apply(context.Background(), d, true); err == nil {
		t.Fatal("expected error")
	}
	if strings.Join(f.actions, ",") != "create" || j.entry.Phase != "creating" {
		t.Fatalf("unexpected partial state: %+v", j.entry)
	}
}

func TestMountWillNotCreate(t *testing.T) {
	d, f, _, _, s := fixture()
	if _, err := s.Apply(context.Background(), d, false); err == nil {
		t.Fatal("mount operation must never create")
	}
	if len(f.actions) != 0 {
		t.Fatal("unexpected mutation")
	}
}

func TestJournalIdentityConflict(t *testing.T) {
	d, f, j, _, s := fixture()
	f.snapshot.Volumes = append(f.snapshot.Volumes, domain.Volume{
		Name: d.Name, UUID: "CURRENT", ContainerUUID: "C", Device: "disk3s2", Encrypted: true,
	})
	j.entry = &state.Entry{Desired: d, VolumeUUID: "OLD"}
	if _, err := s.Apply(context.Background(), d, true); err == nil || !strings.Contains(err.Error(), "UUID differs") {
		t.Fatalf("must refuse conflicting journal identity: %v", err)
	}
}

func TestValidationFailureDoesNotMutate(t *testing.T) {
	d, f, _, l, s := fixture()
	s.ValidateMount = func(string) error { return errors.New("not empty") }
	if _, err := s.Apply(context.Background(), d, true); err == nil {
		t.Fatal("must validate before mutation")
	}
	if l.acquisitions != 0 || len(f.actions) != 0 {
		t.Fatal("validation mutated system")
	}
}
