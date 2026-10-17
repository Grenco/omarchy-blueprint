package autosync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func statusStore(t *testing.T) (StatusStore, string) {
	t.Helper()
	f := newStore(t)
	return StatusStore{StateHome: f.store.StateHome, ProfileRoot: f.store.ProfileRoot}, f.dir
}

func TestStatusStartsEmptyAndNeverCreatesStateOnLoad(t *testing.T) {
	s, dir := statusStore(t)
	st, err := s.Load()
	if err != nil || st.LastCheck != nil || len(st.History) != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("loading status created state")
	}
}

func TestStatusRecordsCheckSeparatelyFromCompletedSync(t *testing.T) {
	s, dir := statusStore(t)
	report := Report{At: t0, Outcome: OutcomeNeedsReview, Reasons: []string{"operations-need-review"}, Counts: Counts{NeedsReview: 2, Automatic: 1}, Fetched: true, Scheduled: true}
	if _, err := s.Record(report); err != nil {
		t.Fatal(err)
	}
	st, err := s.Load()
	if err != nil || st.LastCheck == nil || st.LastCheck.Outcome != OutcomeNeedsReview || st.LastCheck.Counts.NeedsReview != 2 || len(st.History) != 1 {
		t.Fatalf("%+v %v", st, err)
	}
	info, err := os.Stat(filepath.Join(dir, statusFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("status file mode: %v %v", info, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, statusFile))
	if strings.Contains(string(data), "completed") {
		t.Fatalf("a check was recorded as a completed Sync: %s", data)
	}
}

// History is bounded by count, age and bytes, newest first.
func TestStatusHistoryIsBounded(t *testing.T) {
	s, dir := statusStore(t)
	for i := 0; i < historyLimit+40; i++ {
		if _, err := s.Record(Report{At: t0.Add(time.Duration(i) * time.Minute), Outcome: OutcomeUpToDate}); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := s.Load()
	if len(st.History) != historyLimit || !st.History[0].At.Equal(t0.Add(time.Duration(historyLimit+39)*time.Minute)) {
		t.Fatalf("history %d, newest %v", len(st.History), st.History[0].At)
	}
	if _, err := s.Record(Report{At: t0.Add(historyAge + 48*time.Hour), Outcome: OutcomeOffline}); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Load()
	if len(st.History) != 1 {
		t.Fatalf("old history kept: %d", len(st.History))
	}
	info, _ := os.Stat(filepath.Join(dir, statusFile))
	if info.Size() > statusLimit {
		t.Fatalf("status file %d bytes", info.Size())
	}
}

// Only typed, bounded reason codes are stored: nothing that could carry a
// URL, path, profile content or Git output.
func TestStatusStoresOnlySafeReasonCodes(t *testing.T) {
	s, dir := statusStore(t)
	report := Report{At: t0, Outcome: OutcomeError, Reasons: []string{"needs-decision", "https://user:secret@example.invalid", "/home/me/.ssh/id", "fatal: bad\nthing", strings.Repeat("a", 200)}}
	for i := 0; i < 100; i++ {
		report.Reasons = append(report.Reasons, fmt.Sprintf("reason-%d", i))
	}
	saved, err := s.Record(report)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, statusFile))
	for _, leak := range []string{"secret", "example.invalid", ".ssh", "fatal", "aaaa"} {
		if strings.Contains(string(data), leak) {
			t.Fatalf("status stored %q: %s", leak, data)
		}
	}
	if saved.LastCheck.Reasons[0] != "needs-decision" || len(saved.LastCheck.Reasons) > reasonLimit {
		t.Fatalf("%v", saved.LastCheck.Reasons)
	}
}

func TestCorruptStatusRecoversSafely(t *testing.T) {
	s, dir := statusStore(t)
	if _, err := s.Record(Report{At: t0, Outcome: OutcomeUpToDate}); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"garbage": "{not json", "unknown version": `{"version":99}`, "bad outcome": `{"version":1,"last_check":{"outcome":"applied-everything"}}`} {
		if err := os.WriteFile(filepath.Join(dir, statusFile), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		st, err := s.Load()
		if err != nil || !st.Recovered || st.LastCheck != nil {
			t.Fatalf("%s: %+v %v", name, st, err)
		}
		saved, err := s.Record(Report{At: t0, Outcome: OutcomeBusy})
		if err != nil || saved.LastCheck.Outcome != OutcomeBusy || len(saved.History) != 1 {
			t.Fatalf("%s: could not record over corrupt status: %+v %v", name, saved, err)
		}
	}
	// A foreign or symlinked status file is never trusted or followed.
	target := filepath.Join(t.TempDir(), "elsewhere")
	os.WriteFile(target, []byte(`{"version":1}`), 0o600)
	os.Remove(filepath.Join(dir, statusFile))
	os.Symlink(target, filepath.Join(dir, statusFile))
	if st, err := s.Load(); err != nil || !st.Recovered {
		t.Fatalf("symlinked status: %+v %v", st, err)
	}
	if _, err := s.Record(Report{At: t0, Outcome: OutcomeBusy}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != `{"version":1}` {
		t.Fatal("status write followed a symlink")
	}
}

func TestReportJSONUsesTypedCodes(t *testing.T) {
	data, _ := json.Marshal(Report{At: t0, Outcome: OutcomeSignIn, Reasons: []string{"sign-in-needed"}})
	if !strings.Contains(string(data), `"outcome":"sign-in-needed"`) {
		t.Fatalf("%s", data)
	}
}
