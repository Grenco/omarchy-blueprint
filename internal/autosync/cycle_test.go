package autosync

import (
	"context"
	"errors"
	"testing"
	"time"
)

type grantSource struct {
	g   Grant
	err error
}

func (s grantSource) Load() (Grant, bool, error) { return s.g, true, s.err }

type leaseSource struct {
	live bool
	err  error
}

func (s leaseSource) Live(time.Time) (bool, error) { return s.live, s.err }

type fakeNotifier struct {
	notices []Notice
	err     error
}

func (n *fakeNotifier) Notify(_ context.Context, notice Notice) error {
	n.notices = append(n.notices, notice)
	return n.err
}

type cycleFixture struct {
	cycle  Cycle
	checks int
	report Report
	err    error
	notify *fakeNotifier
	status StatusStore
	now    time.Time
}

func newCycle(t *testing.T, g Grant) *cycleFixture {
	t.Helper()
	status, _ := statusStore(t)
	f := &cycleFixture{notify: &fakeNotifier{}, status: status, now: t0, report: Report{Outcome: OutcomeUpToDate, Fetched: true}}
	f.cycle = Cycle{
		Grants:    grantSource{g: g},
		Leases:    leaseSource{},
		Status:    status,
		Notifier:  f.notify,
		Scheduled: true,
		Now:       func() time.Time { return f.now },
		Check: func(context.Context) (Report, error) {
			f.checks++
			return f.report, f.err
		},
	}
	return f
}

func onGrant() Grant {
	g := activeGrant("/p", PresetNotifyOnly)
	return g
}

// Off and Paused report the setting without fetching or recording.
func TestScheduledCycleOffOrPausedDoesNothing(t *testing.T) {
	for _, setting := range []Setting{SettingOff, SettingPaused} {
		g := onGrant()
		g.Setting = setting
		f := newCycle(t, g)
		res, err := f.cycle.Run(context.Background())
		if err != nil || res.Ran || res.Setting != setting || f.checks != 0 {
			t.Fatalf("%s: %+v %v checks=%d", setting, res, err, f.checks)
		}
		if st, _ := f.status.Load(); st.LastCheck != nil {
			t.Fatalf("%s recorded a check", setting)
		}
	}
}

// A check never creates a grant: a missing grant is the default (Off).
func TestScheduledCycleWithoutGrantIsOff(t *testing.T) {
	f := newCycle(t, DefaultGrant())
	res, _ := f.cycle.Run(context.Background())
	if res.Ran || f.checks != 0 || res.Setting != SettingOff {
		t.Fatalf("%+v", res)
	}
}

func TestScheduledCycleSkipsWhileAManualReviewIsOpen(t *testing.T) {
	f := newCycle(t, onGrant())
	f.cycle.Leases = leaseSource{live: true}
	res, err := f.cycle.Run(context.Background())
	if err != nil || f.checks != 0 || res.Report.Outcome != OutcomeBusy || res.Report.Reasons[0] != ReasonReviewOpen || res.Report.Fetched {
		t.Fatalf("%+v %v", res, err)
	}
	if len(f.notify.notices) != 0 {
		t.Fatal("busy check notified")
	}
}

func TestCycleRecordsAndNotifiesOnceForTheSameState(t *testing.T) {
	f := newCycle(t, onGrant())
	f.report = Report{Outcome: OutcomeNeedsReview, Counts: Counts{NeedsReview: 2}, Fetched: true}
	for i := 0; i < 3; i++ {
		f.now = t0.Add(time.Duration(i) * 30 * time.Minute)
		if _, err := f.cycle.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := f.status.Load()
	if len(st.History) != 3 || st.LastCheck.Outcome != OutcomeNeedsReview || !st.LastCheck.Scheduled || !st.LastCheck.At.Equal(f.now) {
		t.Fatalf("%+v", st)
	}
	if len(f.notify.notices) != 1 {
		t.Fatalf("notified %d times", len(f.notify.notices))
	}
	// Up to date in between resets coalescing.
	f.report = Report{Outcome: OutcomeUpToDate, Fetched: true}
	f.cycle.Run(context.Background())
	f.report = Report{Outcome: OutcomeNeedsReview, Counts: Counts{NeedsReview: 2}, Fetched: true}
	f.cycle.Run(context.Background())
	if len(f.notify.notices) != 2 {
		t.Fatalf("notified %d times", len(f.notify.notices))
	}
}

// A notification failure is never a check failure.
func TestNotificationFailureDoesNotChangeTheOutcome(t *testing.T) {
	f := newCycle(t, onGrant())
	f.notify.err = errors.New("no notification daemon")
	f.report = Report{Outcome: OutcomeNeedsReview, Counts: Counts{NeedsReview: 1}}
	res, err := f.cycle.Run(context.Background())
	if err != nil || res.Report.Outcome != OutcomeNeedsReview || res.Notified {
		t.Fatalf("%+v %v", res, err)
	}
	st, _ := f.status.Load()
	if st.Notified != nil || st.LastCheck.Outcome != OutcomeNeedsReview {
		t.Fatalf("%+v", st)
	}
}

// An explicit (person-requested) check runs whatever the setting, and does
// not raise desktop notifications: the person is reading the result.
func TestExplicitCheckRunsWhenOffAndDoesNotNotify(t *testing.T) {
	f := newCycle(t, DefaultGrant())
	f.cycle.Scheduled = false
	f.cycle.Leases = leaseSource{live: true}
	f.report = Report{Outcome: OutcomeNeedsReview, Counts: Counts{NeedsReview: 1}, Fetched: true}
	res, err := f.cycle.Run(context.Background())
	if err != nil || !res.Ran || f.checks != 1 || len(f.notify.notices) != 0 || res.Report.Scheduled {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestUnreadableSettingsAreReportedNotTreatedAsOff(t *testing.T) {
	f := newCycle(t, Grant{})
	f.cycle.Grants = grantSource{err: ErrInvalidGrant}
	res, err := f.cycle.Run(context.Background())
	if err != nil || f.checks != 0 || res.Report.Outcome != OutcomeError || res.Report.Reasons[0] != ReasonSettingsUnreadable || len(f.notify.notices) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestCheckErrorsAreRecordedSafely(t *testing.T) {
	f := newCycle(t, onGrant())
	f.err = errors.New("fatal: https://user:secret@example.invalid broke")
	res, err := f.cycle.Run(context.Background())
	if err != nil || res.Report.Outcome != OutcomeError || res.Report.Reasons[0] != ReasonCheckFailed {
		t.Fatalf("%+v %v", res, err)
	}
}

// Cancellation (systemd stop, Ctrl+C) is returned, not recorded as a result.
func TestCancelledCycleRecordsNothing(t *testing.T) {
	f := newCycle(t, onGrant())
	ctx, cancel := context.WithCancel(context.Background())
	f.cycle.Check = func(context.Context) (Report, error) { cancel(); return Report{}, context.Canceled }
	if _, err := f.cycle.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if st, _ := f.status.Load(); st.LastCheck != nil {
		t.Fatal("cancelled check recorded")
	}
}
