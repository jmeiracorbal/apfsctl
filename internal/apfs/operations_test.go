package apfs

import (
	"context"
	"strings"
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
)

type runnerRecorder struct{ calls [][]string }

func (r *runnerRecorder) Run(_ context.Context, executable string, _ []byte, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{executable}, args...))
	return nil, nil
}

type commandRecorder struct {
	calls [][]string
}

func (r *commandRecorder) RunWithPassphrase(_ context.Context, prompt, executable string, args ...string) error {
	r.calls = append(r.calls, append([]string{prompt, executable}, args...))
	return nil
}

func TestCreateEncryptedArguments(t *testing.T) {
	r := &commandRecorder{}
	m := Mutator{Credentials: r}
	err := m.CreateEncrypted(context.Background(), domain.Container{
		UUID: "C", Reference: "disk3",
	}, "repos")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected one command, got %d", len(r.calls))
	}
	got := strings.Join(r.calls[0], " ")
	if !strings.Contains(got, "apfs addVolume disk3 APFS repos -stdinpassphrase -nomount") {
		t.Fatalf("incorrect APFS command: %s", got)
	}
}

func TestUnlockArguments(t *testing.T) {
	r := &commandRecorder{}
	m := Mutator{Credentials: r}
	err := m.Unlock(context.Background(), domain.Volume{Device: "disk3s2"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.calls[0], " ")
	if !strings.Contains(got, "apfs unlockVolume disk3s2 -stdinpassphrase -nomount") {
		t.Fatalf("unexpected unlock command: %s", got)
	}
}

func TestMountAppliesRequestedFinderVisibility(t *testing.T) {
	runner := &runnerRecorder{}
	mutator := Mutator{Read: Client{Run: runner}}
	volume := domain.Volume{Device: "disk3s7"}
	if err := mutator.Mount(context.Background(), volume, "/Volumes/Proyectos", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(runner.calls[0], " "), "mount nobrowse") {
		t.Fatalf("hidden mount must use nobrowse: %v", runner.calls[0])
	}
	if err := mutator.Mount(context.Background(), volume, "/Volumes/Proyectos", true); err != nil {
		t.Fatal(err)
	}
	visible := strings.Join(runner.calls[1], " ")
	if !strings.Contains(visible, "disk3s7") || strings.Contains(visible, "nobrowse") {
		t.Fatalf("visible mount must omit nobrowse: %v", runner.calls[1])
	}
	if err := mutator.Unmount(context.Background(), volume); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(runner.calls[2], " "), "unmount disk3s7") {
		t.Fatalf("unexpected unmount command: %v", runner.calls[2])
	}
}
