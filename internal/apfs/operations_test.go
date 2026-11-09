package apfs

import (
	"context"
	"strings"
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
)

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
