package autosync

import (
	"context"
	"time"
)

// Reason codes a cycle adds itself; plan reasons come from the check.
const (
	ReasonReviewOpen         = "manual-review-open"
	ReasonProfileInUse       = "profile-in-use"
	ReasonSettingsUnreadable = "settings-unreadable"
	ReasonCheckFailed        = "check-failed"
)

// GrantLoader loads the local grant (Store satisfies it).
type GrantLoader interface {
	Load() (Grant, bool, error)
}

// LeaseChecker reports a live manual-review lease (LeaseStore satisfies it).
type LeaseChecker interface {
	Live(time.Time) (bool, error)
}

// Cycle is one Auto Sync check. It is read-only apart from the tracking ref
// a fetch moves and its own status record: it never creates or changes a
// grant, and Phase 11 PR B has no apply step at all.
type Cycle struct {
	Grants   GrantLoader
	Leases   LeaseChecker
	Status   StatusStore
	Notifier Notifier
	// Check takes the profile lock without waiting (reporting Busy if it is
	// held), fetches once, inspects and classifies the whole plan.
	Check func(context.Context) (Report, error)
	Now   func() time.Time
	// Scheduled: started by the timer rather than a person. Scheduled
	// cycles honour Off/Paused and open reviews, and notify.
	Scheduled bool
}

// CycleResult is what a cycle did.
type CycleResult struct {
	Setting   Setting `json:"setting"`
	Suspended bool    `json:"suspended"`
	// Ran: a check (or a Busy skip) was recorded.
	Ran      bool   `json:"ran"`
	Report   Report `json:"report"`
	Notified bool   `json:"notified"`
}

func (c Cycle) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// Run performs the cycle. Only cancellation is returned as an error; every
// other failure becomes a recorded, sanitized outcome.
func (c Cycle) Run(ctx context.Context) (CycleResult, error) {
	grant, _, err := c.Grants.Load()
	if err != nil {
		// Unreadable settings mean Auto Sync stays off, but a scheduled
		// timer is still installed, so say so rather than staying silent.
		return c.finish(ctx, CycleResult{Setting: SettingOff}, Report{Outcome: OutcomeError, Reasons: []string{ReasonSettingsUnreadable}})
	}
	result := CycleResult{Setting: grant.Setting, Suspended: grant.Suspended != nil}
	if c.Scheduled && grant.Setting != SettingOn {
		return result, nil
	}
	if c.Scheduled && c.Leases != nil {
		if live, err := c.Leases.Live(c.now()); err == nil && live {
			return c.finish(ctx, result, Report{Outcome: OutcomeBusy, Reasons: []string{ReasonReviewOpen}})
		}
	}
	report, err := c.Check(ctx)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		report = Report{Outcome: OutcomeError, Reasons: []string{ReasonCheckFailed}}
	}
	return c.finish(ctx, result, report)
}

func (c Cycle) finish(ctx context.Context, result CycleResult, report Report) (CycleResult, error) {
	report.At, report.Scheduled = c.now(), c.Scheduled
	st, err := c.Status.Record(report)
	if err != nil {
		return result, err
	}
	result.Ran, result.Report = true, *st.LastCheck
	switch {
	case report.Outcome == OutcomeUpToDate || report.Outcome == OutcomeApplied:
		if st.Notified != nil {
			_, _ = c.Status.update(func(s *Status) { s.Notified = nil }, report.At)
		}
	case c.Scheduled && c.Notifier != nil && shouldNotify(st.Notified, result.Report, report.At):
		notice, _ := NoticeFor(result.Report)
		if c.Notifier.Notify(ctx, notice) == nil {
			result.Notified = true
			_, _ = c.Status.MarkNotified(noticeKey(result.Report), report.At)
		}
	}
	return result, nil
}
