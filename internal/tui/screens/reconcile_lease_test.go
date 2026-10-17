package screens

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// coordinatedFixture is a Sync session that also coordinates with Auto
// Sync: a review lease and the tracking revision.
type coordinatedFixture struct {
	reconcileFixture
	holds, heartbeats, releases int
	holding                     bool
	tracking                    string
}

func (f *coordinatedFixture) HoldSyncReview() error {
	if !f.holding {
		f.holds++
	}
	f.holding = true
	return nil
}
func (f *coordinatedFixture) HeartbeatSyncReview() error {
	f.heartbeats++
	return nil
}
func (f *coordinatedFixture) ReleaseSyncReview() error {
	if f.holding {
		f.releases++
	}
	f.holding = false
	return nil
}
func (f *coordinatedFixture) SyncTrackingRevision(context.Context) (string, error) {
	return f.tracking, nil
}

func settledSyncPlan(t *testing.T) syncengine.Plan {
	t.Helper()
	p, err := syncengine.Reconcile(syncengine.PlanRequest{Identity: syncengine.Identity{Base: strings.Repeat("a", 40), Local: strings.Repeat("a", 40), Remote: strings.Repeat("a", 40), Machine: "desktop", Branch: "main", Upstream: "origin/main", RemoteFingerprint: strings.Repeat("d", 64), ControlFingerprint: strings.Repeat("e", 64)}, Git: syncengine.GitState{Clean: true}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// An open review with something to decide holds a lease (so scheduled
// checks skip), heartbeats it, and lets it go once nothing is pending.
func TestReconcileReviewHoldsAndReleasesTheLease(t *testing.T) {
	f := &coordinatedFixture{reconcileFixture: reconcileFixture{plan: conflictSyncPlan(t)}}
	s := NewReconcileContext(context.Background(), f)
	tick := s.Update(s.Init()())
	if f.holds != 1 || !f.holding || tick == nil {
		t.Fatalf("review did not hold a lease: holds=%d tick=%v", f.holds, tick != nil)
	}
	if next := s.Update(reconcileLeaseTickMsg{}); f.heartbeats != 1 || next == nil {
		t.Fatalf("lease not heartbeated: %d", f.heartbeats)
	}
	f.plan = settledSyncPlan(t)
	s.Update(s.Update(tea.KeyPressMsg{Code: 'r'})())
	if f.holding || f.releases != 1 {
		t.Fatal("lease kept with nothing to review")
	}
	if next := s.Update(reconcileLeaseTickMsg{}); next != nil || f.heartbeats != 1 {
		t.Fatal("heartbeat continued after release")
	}
}

func staleReview(t *testing.T) (*coordinatedFixture, *Reconcile) {
	t.Helper()
	f := &coordinatedFixture{reconcileFixture: reconcileFixture{plan: conflictSyncPlan(t)}}
	f.tracking = f.plan.Identity.Remote
	s := NewReconcileContext(context.Background(), f)
	s.SetSize(100, 30)
	s.Update(s.Init()())
	s.Update(tea.KeyPressMsg{Code: 'L'})
	if !s.CanExecute() {
		t.Fatal("fixture: decided review not runnable")
	}
	return f, s
}

func TestReconcileConfirmsWhenTheSharedProfileHasNotMoved(t *testing.T) {
	_, s := staleReview(t)
	s.Update(s.Update(tea.KeyPressMsg{Code: 's'})())
	if !s.confirming {
		t.Fatal("unchanged review did not reach confirmation")
	}
}

// New shared changes during the review: say so, offer Refresh, keep
// decisions that still apply.
func TestReconcileStaleReviewKeepsStillValidDecisions(t *testing.T) {
	f, s := staleReview(t)
	f.tracking = strings.Repeat("f", 40)
	s.Update(s.Update(tea.KeyPressMsg{Code: 's'})())
	if s.confirming || s.CanExecute() || f.executions != 0 {
		t.Fatal("stale review reached confirmation")
	}
	if flat := strings.Join(strings.Fields(s.View()), " "); !strings.Contains(flat, "New changes arrived from your shared profile while you were reviewing. Your decisions are kept where they still apply.") {
		t.Fatalf("stale review not explained:\n%s", s.View())
	}
	refresh := false
	for _, action := range s.Actions() {
		if action.Shortcut == "r" && action.Label == "Refresh" && action.Enabled {
			refresh = true
		}
	}
	if !refresh {
		t.Fatalf("no Refresh action: %+v", s.Actions())
	}
	// The setting itself did not change: the decision is kept.
	f.plan.Identity.Remote = f.tracking
	s.Update(s.Update(tea.KeyPressMsg{Code: 'r'})())
	if len(s.choices) != 1 || !s.CanExecute() {
		t.Fatalf("still-valid decision dropped: %v", s.choices)
	}
	if strings.Contains(s.View(), "New changes arrived") {
		t.Fatal("stale notice kept after refresh")
	}
}

func TestReconcileStaleReviewDropsChangedDecisions(t *testing.T) {
	f, s := staleReview(t)
	f.tracking = strings.Repeat("f", 40)
	s.Update(s.Update(tea.KeyPressMsg{Code: 's'})())
	changed := conflictSyncPlan(t)
	changed.Identity.Remote = f.tracking
	changed.Items[0].Remote.Fingerprint = "newer-remote"
	f.plan = changed
	s.Update(s.Update(tea.KeyPressMsg{Code: 'r'})())
	if len(s.choices) != 0 || s.CanExecute() {
		t.Fatalf("decision kept for a setting that changed: %v", s.choices)
	}
}

// An ordinary check for updates still starts a fresh review.
func TestReconcileOrdinaryRefreshStartsAFreshReview(t *testing.T) {
	_, s := staleReview(t)
	s.Update(s.Update(tea.KeyPressMsg{Code: 'r'})())
	if len(s.choices) != 0 {
		t.Fatal("an ordinary refresh replayed decisions")
	}
}

// Manual Sync finding the profile in use (an Auto Sync check that started
// before the review opened) says nothing was changed, not that work may be
// partly done.
func TestReconcileBusyProfileSaysNothingChanged(t *testing.T) {
	msg := reconcileFailure(fmt.Errorf("wrapped: %w", syncengine.ErrProfileBusy))
	if !strings.Contains(msg, "Nothing was changed") || strings.Contains(msg, "may already have been sent") {
		t.Fatal(msg)
	}
}
