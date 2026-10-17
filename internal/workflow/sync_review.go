package workflow

import (
	"context"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/autosync"
)

func (s *Session) leaseStore() (autosync.LeaseStore, func() time.Time, error) {
	s.configMu.Lock()
	deps, root := s.deps, s.opts.ProfileDir
	s.configMu.Unlock()
	state, err := deps.StateHome()
	if err != nil {
		return autosync.LeaseStore{}, nil, err
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return autosync.LeaseStore{StateHome: state, ProfileRoot: root}, now, nil
}

// HoldSyncReview records that a person has a Sync review open, so
// scheduled Auto Sync checks skip fetching. Holding twice is harmless.
func (s *Session) HoldSyncReview() error {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	if s.reviewLease != nil {
		return nil
	}
	store, now, err := s.leaseStore()
	if err != nil {
		return err
	}
	lease, err := store.Hold(now())
	if err != nil {
		return err
	}
	s.reviewLease = lease
	return nil
}

// HeartbeatSyncReview renews a held review lease.
func (s *Session) HeartbeatSyncReview() error {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	if s.reviewLease == nil {
		return nil
	}
	_, now, err := s.leaseStore()
	if err != nil {
		return err
	}
	return s.reviewLease.Heartbeat(now())
}

// ReleaseSyncReview ends a held review lease.
func (s *Session) ReleaseSyncReview() error {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	lease := s.reviewLease
	s.reviewLease = nil
	return lease.Release()
}

// SyncTrackingRevision reads the remote-tracking revision without fetching.
func (s *Session) SyncTrackingRevision(ctx context.Context) (string, error) {
	s.configMu.Lock()
	git := s.profileGit
	s.configMu.Unlock()
	return git.UpstreamCommit(ctx)
}
