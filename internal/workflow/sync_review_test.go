package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/autosync"
)

// The TUI's Sync review holds a lease through the session; scheduled checks
// see it. The tracking revision is read without fetching.
func TestSyncReviewLeaseAndTrackingRevision(t *testing.T) {
	s, _, spy, peer, g := autoCheckFixture(t)
	_ = peer
	state, _ := s.deps.StateHome()
	leases := autosync.LeaseStore{StateHome: state, ProfileRoot: s.ProfileDir()}
	if err := s.HoldSyncReview(); err != nil {
		t.Fatal(err)
	}
	if err := s.HoldSyncReview(); err != nil {
		t.Fatal("holding twice:", err)
	}
	if live, _ := leases.Live(time.Now()); !live {
		t.Fatal("review lease not visible to checks")
	}
	if err := s.HeartbeatSyncReview(); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseSyncReview(); err != nil {
		t.Fatal(err)
	}
	if live, _ := leases.Live(time.Now()); live {
		t.Fatal("lease outlived release")
	}
	if err := s.ReleaseSyncReview(); err != nil || s.HeartbeatSyncReview() != nil {
		t.Fatal("release/heartbeat without a lease must be harmless")
	}
	before, err := s.SyncTrackingRevision(context.Background())
	if err != nil || len(before) != 40 || spy.fetches() != 0 {
		t.Fatalf("%q %v fetches=%d", before, err, spy.fetches())
	}
	if _, err := s.CheckAutoSync(context.Background(), s.testCheckOptions(g)); err != nil {
		t.Fatal(err)
	}
	after, _ := s.SyncTrackingRevision(context.Background())
	if after == before {
		t.Fatal("tracking revision did not reflect the fetch")
	}
}
