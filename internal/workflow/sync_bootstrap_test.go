package workflow

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncBootstrapProvesConvergenceWithoutWritingCursor(t *testing.T) {
	s, runner, _, base, _, binds, observes := syncTwoClones(t)
	state, _ := s.deps.StateHome()
	store := syncengine.CursorStore{StateHome: state, ProfileRoot: s.ProfileDir()}
	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
	plan, err := s.InspectSyncBootstrap(context.Background(), SyncInspectOptions{Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ConvergenceEligible || plan.Identity.Local != base || plan.Identity.Remote != base || plan.Identity.Base != "" {
		t.Fatal(plan)
	}
	if _, exists, err := store.Load(); err != nil || exists {
		t.Fatal("bootstrap inspection wrote cursor", err)
	}
	if runner.fetches.Load() != 1 || binds.Load() != 1 || observes.Load() != 1 {
		t.Fatal("bootstrap repeated observation")
	}
	for _, choice := range []string{"reconcile-upstream", "adopt-machine", "cancel"} {
		found := false
		for _, x := range plan.Choices {
			if x == choice {
				found = true
			}
		}
		if !found {
			t.Fatal("missing explicit bootstrap choice", choice)
		}
	}
}

func TestSyncBootstrapDriftAndUnsharedRevisionNotAutomaticallyEligible(t *testing.T) {
	for _, kind := range []string{"drift", "unshared", "dirty"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _, _, _, _, _ := syncTwoClones(t)
			switch kind {
			case "drift":
				s.providers[0].(*syncLiveFake).live.Packages.Official = append(s.providers[0].(*syncLiveFake).live.Packages.Official, "extra")
			case "unshared":
				syncGit(t, s.ProfileDir(), "commit", "--allow-empty", "-m", "unpublished")
			case "dirty":
				if err := os.WriteFile(filepath.Join(s.ProfileDir(), "unmanaged.txt"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := s.InspectSyncBootstrap(context.Background(), SyncInspectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if plan.ConvergenceEligible {
				t.Fatal("unsettled bootstrap eligible", kind, plan)
			}
		})
	}
}
