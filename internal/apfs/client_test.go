package apfs

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls       []string
	bad         bool
	mountOutput []byte
}

func (f *fakeRunner) Run(_ context.Context, exe string, stdin []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, exe+" "+strings.Join(args, " "))
	if exe == plutil {
		return stdin, nil
	}
	if strings.Contains(strings.Join(args, " "), "apfs list") {
		return []byte(`{"Containers":[{"APFSContainerUUID":"CONT","ContainerReference":"disk2","CapacityFree":524288,"Volumes":[{"Name":"repos","APFSVolumeUUID":"VOL","DeviceIdentifier":"disk2s5","Encryption":true,"Locked":false,"Roles":[]}]}]}`), nil
	}
	if exe == "/sbin/mount" {
		if f.mountOutput != nil {
			return f.mountOutput, nil
		}
		return []byte("/dev/disk2s5 on /Users/test/repos (apfs, local, nodev, nosuid, journaled, nobrowse)\n"), nil
	}
	if f.bad {
		return nil, fmt.Errorf("disk failure")
	}
	return []byte(`{"MountPoint":"/Users/test/repos"}`), nil
}

func TestMountedVolumesReadsFinderVisibility(t *testing.T) {
	runner := &fakeRunner{mountOutput: []byte(
		"/dev/disk2s5 on /Volumes/Visible (apfs, local, nodev)\n" +
			"/dev/disk2s6 on /Volumes/Hidden (apfs, local, nodev, nobrowse)\n",
	)}
	got, err := (Client{Run: runner}).mountedVolumes(context.Background())
	if err != nil || !got["disk2s5"].browsable || got["disk2s6"].browsable || got["disk2s6"].path != "/Volumes/Hidden" {
		t.Fatalf("unexpected mount visibility: %+v, %v", got, err)
	}
}

func TestSnapshotStructuredData(t *testing.T) {
	f := &fakeRunner{}
	s, err := (Client{Run: f}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Containers) != 1 || s.Containers[0].Reference != "disk2" {
		t.Fatalf("invalid container: %+v", s)
	}
	if len(s.Volumes) != 1 || !s.Volumes[0].Encrypted || s.Volumes[0].MountPoint != "/Users/test/repos" || s.Volumes[0].Browsable == nil || *s.Volumes[0].Browsable {
		t.Fatalf("snapshot: %+v", s)
	}
	if len(f.calls) != 5 {
		t.Fatalf("expected 5 typed commands, got %+v", f.calls)
	}
}

func TestFailsClosedOnDiskError(t *testing.T) {
	_, err := (Client{Run: &fakeRunner{bad: true}}).Snapshot(context.Background())
	if err == nil {
		t.Fatal("failed observation must not yield empty snapshot")
	}
}
