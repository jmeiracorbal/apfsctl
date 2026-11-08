package provision

import (
	"context"
	"strings"
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/state"
)

func TestFailedCreationCanRetryWhenVolumeIsAbsent(t *testing.T) {
	d, f, j, _, s := fixture()
	f.fail = "create"
	j.entry = &state.Entry{Desired: d, Phase: "creating"}
	if _, err := s.Apply(context.Background(), d, true); err == nil {
		t.Fatal("first create attempt should fail")
	}
	if j.entry.Phase != "creating" || strings.Join(f.actions, ",") != "create" {
		t.Fatalf("unexpected failed-create state: entry=%+v actions=%v", j.entry, f.actions)
	}

	f.fail = ""
	out, err := s.Apply(context.Background(), d, true)
	if err != nil {
		t.Fatalf("create should retry after the snapshot confirms the volume is absent: %v", err)
	}
	if out.Noop || out.VolumeUUID != "NEW" || strings.Join(f.actions, ",") != "create,create,unlock,mount" {
		t.Fatalf("unexpected retry result: result=%+v actions=%v", out, f.actions)
	}
}
