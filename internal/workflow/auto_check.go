package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/autosync"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// DefaultFetchTimeout bounds one Auto Sync fetch.
const DefaultFetchTimeout = 2 * time.Minute

// Reason codes for checks that could not finish.
const (
	ReasonFetchTimedOut     = "fetch-timed-out"
	ReasonRemoteUnreachable = "remote-unreachable"
	ReasonSignInNeeded      = "sign-in-needed"
	ReasonApplyUnavailable  = "automatic-apply-not-available-yet"
)

// AutoSyncCheckOptions configures one read-only check.
type AutoSyncCheckOptions struct {
	Grant        autosync.Grant
	HostID       string
	Registry     autosync.Registry
	Evidence     autosync.Evidence
	FetchTimeout time.Duration
}

// errFetchTimedOut marks a fetch stopped by its own timeout (not by the
// caller's cancellation).
var errFetchTimedOut = errors.New("fetch timed out")

// CheckAutoSync takes the profile lock without waiting (a held lock is a
// Busy skip), fetches once, inspects and classifies the whole plan. It
// never applies, captures, publishes, fast-forwards or writes the cursor.
// Only cancellation is returned as an error.
func (s *Session) CheckAutoSync(ctx context.Context, opts AutoSyncCheckOptions) (autosync.Report, error) {
	return s.checkAutoSyncWith(ctx, opts, nil)
}

// checkAutoSyncWith runs afterLock (tests only) once the lock is held.
func (s *Session) checkAutoSyncWith(ctx context.Context, opts AutoSyncCheckOptions, afterLock func()) (autosync.Report, error) {
	preview, err := s.previewAutoSync(ctx, AutoSyncPreviewOptions{Fetch: true, Grant: opts.Grant, HostID: opts.HostID, Registry: opts.Registry, Evidence: opts.Evidence, fetchTimeout: opts.FetchTimeout, afterLock: afterLock})
	if ctx.Err() != nil {
		return autosync.Report{}, ctx.Err()
	}
	switch {
	case errors.Is(err, syncengine.ErrProfileBusy):
		return autosync.Report{Outcome: autosync.OutcomeBusy, Reasons: []string{autosync.ReasonProfileInUse}}, nil
	case errors.Is(err, errFetchTimedOut):
		return autosync.Report{Outcome: autosync.OutcomeOffline, Reasons: []string{ReasonFetchTimedOut}}, nil
	case errors.Is(err, profilegit.ErrFetchUnreachable):
		return autosync.Report{Outcome: autosync.OutcomeOffline, Reasons: []string{ReasonRemoteUnreachable}}, nil
	case errors.Is(err, profilegit.ErrFetchSignIn):
		return autosync.Report{Outcome: autosync.OutcomeSignIn, Reasons: []string{ReasonSignInNeeded}}, nil
	case err != nil:
		return autosync.Report{Outcome: autosync.OutcomeError, Reasons: []string{autosync.ReasonCheckFailed}}, nil
	}
	return ReportFromPreview(preview), nil
}

// ReportFromPreview turns a preview into a check outcome. Phase 11 PR B has
// no executor, so even a fully eligible plan waits for review.
func ReportFromPreview(p AutoSyncPreview) autosync.Report {
	r := autosync.Report{Fetched: p.Fetched, Counts: autosync.Counts{Automatic: p.Counts.Automatic, NeedsReview: p.Counts.NeedsReview, Matching: p.Counts.Matching, Preserved: p.Counts.Preserved}}
	if p.UpToDate && len(p.Blockers) == 0 {
		r.Outcome = autosync.OutcomeUpToDate
		return r
	}
	r.Outcome = autosync.OutcomeNeedsReview
	for _, reason := range p.Reasons {
		r.Reasons = append(r.Reasons, string(reason))
	}
	r.Reasons = append(r.Reasons, p.Blockers...)
	if p.Eligible {
		r.Reasons = append(r.Reasons, ReasonApplyUnavailable)
	}
	return r
}

// fetchSyncUpstream fetches the Sync upstream within timeout.
func (s *Session) fetchSyncUpstream(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultFetchTimeout
	}
	s.configMu.Lock()
	git := s.profileGit
	s.configMu.Unlock()
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := git.FetchUpstream(fetchCtx)
	if err != nil && ctx.Err() == nil && fetchCtx.Err() != nil {
		return errFetchTimedOut
	}
	return err
}
