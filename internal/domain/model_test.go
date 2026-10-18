package domain

import "testing"

func TestDesiredValidation(t *testing.T) {
	d := Desired{Name: "repos", ContainerUUID: "C", MountPoint: "/Users/test/repos", Encrypted: true}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "-repos", "bad/name", "bad:name"} {
		d.Name = name
		if err := d.Validate(); err == nil {
			t.Errorf("accepted invalid name %q", name)
		}
	}
}

func TestSnapshotRejectsCorruption(t *testing.T) {
	s := Snapshot{Containers: []Container{{UUID: "C", Reference: "disk3"}},
		Volumes: []Volume{{UUID: "V", Name: "repos", ContainerUUID: "C", Device: "disk3s1", Encrypted: true}}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.Volumes[0].ContainerUUID = "UNKNOWN"
	if err := s.Validate(); err == nil {
		t.Fatal("unknown APFS container must fail")
	}
	s.Volumes[0].ContainerUUID = "C"
	s.Volumes[0].MountPoint = "/Users/test/repos"
	s.Volumes[0].Locked = true
	if err := s.Validate(); err == nil {
		t.Fatal("locked and mounted state must fail")
	}
}
