package provision

import (
	"context"
	"strings"
	"testing"

	"github.com/jmeiracorbal/verbose-octo-spork/internal/state"
)

func TestAmbiguousPreviousCreationCannotRetry(t *testing.T) {
	d, f, j, _, s := fixture()
	j.entry = &state.Entry{Desired: d, Phase: "creating"}
	if _, err := s.Apply(context.Background(), d, true); err == nil ||
		!strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("unresolved previous creation must stop safely, got: %v", err)
	}
	if len(f.actions) != 0 {
		t.Fatalf("unexpected retry after ambiguous state: %v", f.actions)
	}
}
