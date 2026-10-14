package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// syncMissingThemeArtifact reports the real-machine failure shape: a recorded
// local theme whose artifact Git could not carry.
type syncMissingThemeArtifact struct{ statusTestProvider }

func (syncMissingThemeArtifact) ValidateSyncArtifacts(context.Context, string, profile.Data) error {
	return &syncengine.ArtifactError{Target: "theme:aether", Missing: true}
}

func TestSyncHistoricalLoadFailureNamesStageAndNeverAuthorizesExecution(t *testing.T) {
	s, _, _, _, _ := executionSyncFixture(t)
	before := syncCursorForTest(t, s)
	s.providers = append(s.providers, syncMissingThemeArtifact{statusTestProvider{id: "themes"}})

	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertArtifactBlocker(t, plan.Blockers, "Last-sync (B)")
	if _, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan}); err == nil {
		t.Fatal("blocked inspection authorized execution")
	}
	if after := syncCursorForTest(t, s); after != before {
		t.Fatal("failed inspection moved the cursor")
	}

	state, _ := s.deps.StateHome()
	store := syncengine.CursorStore{StateHome: state, ProfileRoot: s.ProfileDir()}
	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
	review, err := s.InspectSyncBootstrap(context.Background(), SyncInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertArtifactBlocker(t, review.Blockers, "Pinned local (L)")
	if review.ConvergenceEligible {
		t.Fatal("unloadable revision claimed convergence")
	}
	choice := SyncBootstrapChoice("reconcile-upstream")
	if result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{Bootstrap: &choice, ReviewedBootstrap: &review}); err == nil || result.CursorAdvanced {
		t.Fatal("blocked bootstrap executed")
	}
	if _, ok, _ := store.Load(); ok {
		t.Fatal("blocked bootstrap wrote a cursor")
	}
}

func assertArtifactBlocker(t *testing.T, blockers []syncengine.Blocker, which string) {
	t.Helper()
	for _, b := range blockers {
		if b.Code == "historical-profile" {
			for _, want := range []string{which, "themes", "theme:aether", "artifact is missing", "Stop managing theme:aether", "Capture policy to preserve"} {
				if !strings.Contains(b.Message, want) {
					t.Fatalf("blocker %q lacks %q", b.Message, want)
				}
			}
			return
		}
	}
	t.Fatalf("missing historical-profile blocker: %v", blockers)
}

// Each stage reports itself, and only safe identities leave the boundary.
func TestSyncViewBlockerDistinguishesStagesWithoutLeakingCauses(t *testing.T) {
	leak := errors.New("/home/private/profile: secret-token")
	cases := map[SyncViewStage]string{
		SyncViewSnapshot:   "could not be read from local Git objects",
		SyncViewProfile:    "profile files are invalid",
		SyncViewPolicy:     "policy names targets",
		SyncViewArtifacts:  "committed hooks artifacts do not match",
		SyncViewProjection: "hooks targets cannot be projected",
		SyncViewControl:    "control state cannot be read",
	}
	for stage, want := range cases {
		err := syncViewFailure(context.Background(), stage, "hooks", leak)
		b := syncViewBlocker("Pinned local (L)", err)
		if b.Code != "historical-profile" || !strings.Contains(b.Message, want) || strings.Contains(b.Message, "private") || strings.Contains(b.Message, "secret") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: %q", stage, b.Message)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := syncViewFailure(ctx, SyncViewSnapshot, "", leak); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation became a profile diagnostic", err)
	}
}
