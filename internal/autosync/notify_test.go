package autosync

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNoticesArePlainEnglishCountsOnly(t *testing.T) {
	n, ok := NoticeFor(Report{Outcome: OutcomeNeedsReview, Counts: Counts{Automatic: 1, NeedsReview: 2}, Reasons: []string{"operations-need-review"}})
	if !ok || n.Title != "Blueprint: Changes need review" || !strings.Contains(n.Body, "3 changes were found in your shared setup") || !strings.Contains(n.Body, "Nothing was applied automatically") || !strings.Contains(n.Body, "Blueprint → Sync") {
		t.Fatalf("%+v", n)
	}
	n, _ = NoticeFor(Report{Outcome: OutcomeNeedsReview, Counts: Counts{NeedsReview: 1}})
	if !strings.Contains(n.Body, "1 change was found") {
		t.Fatalf("%+v", n)
	}
	for _, outcome := range []Outcome{OutcomeSignIn, OutcomeOffline, OutcomeError} {
		if n, ok := NoticeFor(Report{Outcome: outcome}); !ok || n.Title == "" || !strings.Contains(n.Body, "nothing was changed") {
			t.Fatalf("%s: %+v", outcome, n)
		}
	}
	for _, outcome := range []Outcome{OutcomeUpToDate, OutcomeBusy} {
		if _, ok := NoticeFor(Report{Outcome: outcome}); ok {
			t.Fatalf("%s notified", outcome)
		}
	}
}

// Identical states coalesce; a changed state or the reminder interval
// notifies again; coming back up to date resets coalescing.
func TestNotificationsCoalesce(t *testing.T) {
	review := Report{Outcome: OutcomeNeedsReview, Counts: Counts{NeedsReview: 2}}
	key := noticeKey(review)
	if !shouldNotify(nil, review, t0) {
		t.Fatal("first review not notified")
	}
	last := &Notified{Key: key, At: t0}
	if shouldNotify(last, review, t0.Add(time.Hour)) {
		t.Fatal("identical review notified again")
	}
	changed := review
	changed.Counts.NeedsReview = 3
	if !shouldNotify(last, changed, t0.Add(time.Hour)) {
		t.Fatal("changed review not notified")
	}
	if !shouldNotify(last, review, t0.Add(reminderInterval+time.Minute)) {
		t.Fatal("no reminder after the interval")
	}
	offline := Report{Outcome: OutcomeOffline}
	if shouldNotify(&Notified{Key: noticeKey(offline), At: t0}, offline, t0.Add(2*time.Hour)) {
		t.Fatal("recurring offline warning not coalesced")
	}
}

type recordingRunner struct {
	calls [][]string
	err   error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return "", r.err
}

func TestDesktopNotifierPassesTextAsArgumentsAndEscapesMarkup(t *testing.T) {
	r := &recordingRunner{}
	if err := (DesktopNotifier{Runner: r}).Notify(context.Background(), Notice{Title: "A <b>", Body: "x & <a href='y'>"}); err != nil {
		t.Fatal(err)
	}
	call := r.calls[0]
	if call[0] != "notify-send" || call[len(call)-3] != "--" || call[len(call)-2] != "A &lt;b&gt;" || call[len(call)-1] != "x &amp; &lt;a href=&#39;y&#39;&gt;" {
		t.Fatalf("%q", call)
	}
}
