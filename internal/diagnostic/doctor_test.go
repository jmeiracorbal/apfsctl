package diagnostic

import (
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
)

func TestHealthyWhenConverged(t *testing.T) {
	d := domain.Desired{Name: "repos", ContainerUUID: "C", MountPoint: "/Users/test/repos", Encrypted: true}
	s := domain.Snapshot{
		Containers: []domain.Container{{UUID: "C", Reference: "disk2"}},
		Volumes: []domain.Volume{{UUID: "V", Name: "repos", Device: "disk2s1",
			ContainerUUID: "C", Encrypted: true, MountPoint: d.MountPoint}},
	}
	r := Inspect(s, &d)
	if !r.Healthy || r.Plan == nil || !r.Plan.Noop {
		t.Fatalf("unexpected report: %+v", r)
	}
	s.Volumes[0].MountPoint = ""
	r = Inspect(s, &d)
	if r.Healthy || r.Plan == nil || r.Plan.Noop || r.Findings[0].Code != "drift" {
		t.Fatalf("drift should be reported: %+v", r)
	}
	s.Volumes[0].Encrypted = false
	r = Inspect(s, &d)
	if r.Healthy || r.Findings[0].Code != "conflict" {
		t.Fatalf("conflict should be reported: %+v", r)
	}
}
