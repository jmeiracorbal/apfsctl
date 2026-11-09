package reconcile

import (
	"strings"
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
)

func desired() domain.Desired {
	return domain.Desired{Name: "repos", ContainerUUID: "CONTAINER", MountPoint: "/Users/test/repos", Encrypted: true}
}

func snapshot(vols ...domain.Volume) domain.Snapshot {
	return domain.Snapshot{Containers: []domain.Container{{UUID: "CONTAINER", Reference: "disk3"}}, Volumes: vols}
}

func volume() domain.Volume {
	browsable := false
	return domain.Volume{Name: "repos", UUID: "VOL-1", ContainerUUID: "CONTAINER", Device: "disk3s1", Encrypted: true, MountPoint: "/Users/test/repos", Browsable: &browsable}
}

func TestVisibilityDriftUnmountsAndRemounts(t *testing.T) {
	d := desired()
	d.Browsable = true
	p, err := Build(d, snapshot(volume()))
	if err != nil || p.Noop || len(p.Changes) != 2 || p.Changes[0].Action != Unmount || p.Changes[1].Action != Mount {
		t.Fatalf("visibility drift should remount the volume: %+v, %v", p, err)
	}
}

func TestNoopWhenConverged(t *testing.T) {
	p, err := Build(desired(), snapshot(volume()))
	if err != nil || !p.Noop || len(p.Changes) != 0 {
		t.Fatalf("unexpected plan: %+v, %v", p, err)
	}
	p2, err := Build(desired(), snapshot(volume()))
	if err != nil || !p2.Noop {
		t.Fatalf("repeated reconcile must remain noop: %+v, %v", p2, err)
	}
}

func TestCreatesOnlyWhenMissing(t *testing.T) {
	p, err := Build(desired(), snapshot())
	if err != nil || p.Noop || len(p.Changes) != 2 || p.Changes[0].Action != CreateEncrypted || p.Changes[1].Action != Mount {
		t.Fatalf("unexpected plan: %+v, %v", p, err)
	}
}

func TestExistingEncryptedVolumeIsNotRecreated(t *testing.T) {
	v := volume()
	v.MountPoint = ""
	v.Locked = true
	p, err := Build(desired(), snapshot(v))
	if err != nil || len(p.Changes) != 2 || p.Changes[0].Action != Unlock || p.Changes[1].Action != Mount {
		t.Fatalf("unexpected plan: %+v, %v", p, err)
	}
}

func TestConflictsFailClosed(t *testing.T) {
	tests := []struct {
		name string
		vols []domain.Volume
		want string
	}{
		{"unencrypted", func() []domain.Volume { v := volume(); v.Encrypted = false; return []domain.Volume{v} }(), "encryption"},
		{"wrong container", func() []domain.Volume { v := volume(); v.ContainerUUID = "OTHER"; return []domain.Volume{v} }(), "unknown container"},
		{"wrong mount", func() []domain.Volume { v := volume(); v.MountPoint = "/Volumes/repos"; return []domain.Volume{v} }(), "already mounted"},
		{"occupied", []domain.Volume{{Name: "other", UUID: "VOL-2", ContainerUUID: "CONTAINER", Device: "disk3s2", Encrypted: true, MountPoint: "/Users/test/repos"}}, "occupied"},
		{"duplicate names", []domain.Volume{volume(), {Name: "repos", UUID: "VOL-2", ContainerUUID: "CONTAINER", Device: "disk3s2", Encrypted: true}}, "multiple volumes"},
		{"role collision", func() []domain.Volume { v := volume(); v.Roles = []string{"Data"}; return []domain.Volume{v} }(), "reserved APFS roles"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := snapshot(tt.vols...)
			if tt.name == "wrong container" {
				s.Containers = append(s.Containers, domain.Container{UUID: "OTHER", Reference: "disk4"})
			}
			_, err := Build(desired(), s)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				if tt.name == "wrong container" && err != nil && strings.Contains(err.Error(), "different APFS container") {
					return
				}
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
}

func TestMissingContainer(t *testing.T) {
	_, err := Build(desired(), domain.Snapshot{Containers: []domain.Container{{UUID: "OTHER", Reference: "disk4"}}})
	if err == nil {
		t.Fatal("missing container must fail")
	}
}
