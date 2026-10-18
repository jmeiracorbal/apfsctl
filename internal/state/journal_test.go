package state

import (
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
)

func TestJournalRoundTrip(t *testing.T) {
	s := FileJournal{Directory: t.TempDir()}
	d := domain.Desired{Name: "repos", ContainerUUID: "C", MountPoint: "/Users/test/repos", Encrypted: true}
	// Save requires root-owned private directory by design and is tested in the
	// privileged integration suite. Load must correctly distinguish absence.
	got, err := s.Load(d)
	if err != nil || got != nil {
		t.Fatalf("unexpected journal: %+v, %v", got, err)
	}
	if s.path(d) == s.path(domain.Desired{Name: "other", ContainerUUID: "C"}) {
		t.Fatal("journal keys must vary with volume identity")
	}
}
