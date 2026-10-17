package workflow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/autosync"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// gitSpyRunner records every Git subcommand and can make fetch fail or hang.
type gitSpyRunner struct {
	*executionSyncRunner
	subcommands []string
	fetchOutput string
	fetchHangs  bool
}

func gitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-C" || args[i] == "-c":
			i++
		case strings.HasPrefix(args[i], "-"):
		default:
			return args[i]
		}
	}
	return ""
}

func (r *gitSpyRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "git" {
		sub := gitSubcommand(args)
		r.subcommands = append(r.subcommands, sub)
		if sub == "fetch" && r.fetchHangs {
			<-ctx.Done()
			return "", &command.RunError{Name: name, Args: args, ExitCode: -1, Err: ctx.Err()}
		}
		if sub == "fetch" && r.fetchOutput != "" {
			return r.fetchOutput, &command.RunError{Name: name, Args: args, Output: r.fetchOutput, ExitCode: 128, Err: errors.New("exit status 128")}
		}
	}
	return r.executionSyncRunner.Run(ctx, name, args...)
}

func (r *gitSpyRunner) fetches() int {
	n := 0
	for _, sub := range r.subcommands {
		if sub == "fetch" {
			n++
		}
	}
	return n
}

// writes are the Git subcommands a check must never run.
var gitWrites = map[string]bool{"push": true, "commit": true, "merge": true, "pull": true, "reset": true, "checkout": true, "switch": true, "rebase": true, "update-ref": true, "add": true, "rm": true, "stash": true, "tag": true, "cherry-pick": true, "restore": true, "clean": true, "am": true, "apply": true}

func (r *gitSpyRunner) assertNoWrites(t *testing.T) {
	t.Helper()
	for _, sub := range r.subcommands {
		if gitWrites[sub] {
			t.Fatalf("check ran git %s: %v", sub, r.subcommands)
		}
	}
}

func autoCheckFixture(t *testing.T) (*Session, *executionSyncProvider, *gitSpyRunner, string, autosync.Grant) {
	t.Helper()
	s, p, r, d, peer := executionSyncFixture(t)
	spy := &gitSpyRunner{executionSyncRunner: r}
	s.deps.Runner, s.profileGit.Runner = spy, spy
	remoteChange(t, d, peer)
	return s, p, spy, peer, autoSyncGrant(t, s, autosync.PresetNotifyOnly)
}

func (s *Session) testCheckOptions(g autosync.Grant) AutoSyncCheckOptions {
	return AutoSyncCheckOptions{Grant: g, HostID: autoSyncTestHost}
}

// A profile lock held by a manual Sync makes the check Busy: no fetch, no
// ref movement, nothing else.
func TestAutoCheckSkipsAsBusyWhileManualSyncHoldsTheLock(t *testing.T) {
	s, p, spy, _, g := autoCheckFixture(t)
	state, _ := s.deps.StateHome()
	lock, err := syncengine.AcquireProfileLock(state, s.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	before := takeAutoSyncSnapshot(t, s, p)
	report, err := s.CheckAutoSync(context.Background(), s.testCheckOptions(g))
	if err != nil || report.Outcome != autosync.OutcomeBusy || report.Reasons[0] != autosync.ReasonProfileInUse || report.Fetched {
		t.Fatalf("%+v %v", report, err)
	}
	if spy.fetches() != 0 || !reflect.DeepEqual(before, takeAutoSyncSnapshot(t, s, p)) {
		t.Fatal("busy check fetched or wrote")
	}
	// A fetching preview takes the same lock.
	if _, err := s.PreviewAutoSync(context.Background(), AutoSyncPreviewOptions{Fetch: true, Grant: g, HostID: autoSyncTestHost}); !errors.Is(err, syncengine.ErrProfileBusy) {
		t.Fatalf("fetching preview ignored the lock: %v", err)
	}
}

// Two checks at once: the second finds the lock held and skips.
func TestAutoCheckConcurrentChecksDoNotBothFetch(t *testing.T) {
	s, _, spy, _, g := autoCheckFixture(t)
	release := make(chan struct{})
	entered := make(chan struct{})
	spy.fetchHangs = false
	first := make(chan autosync.Report, 1)
	go func() {
		report, _ := s.checkAutoSyncWith(context.Background(), s.testCheckOptions(g), func() { close(entered); <-release })
		first <- report
	}()
	<-entered
	second, err := s.CheckAutoSync(context.Background(), s.testCheckOptions(g))
	close(release)
	if err != nil || second.Outcome != autosync.OutcomeBusy {
		t.Fatalf("second check: %+v %v", second, err)
	}
	if r := <-first; r.Outcome == autosync.OutcomeBusy || spy.fetches() != 1 {
		t.Fatalf("first check %+v, fetches %d", r, spy.fetches())
	}
}

func TestAutoCheckFetchesOnceAndReportsWithoutWriting(t *testing.T) {
	s, p, spy, _, g := autoCheckFixture(t)
	before := takeAutoSyncSnapshot(t, s, p)
	report, err := s.CheckAutoSync(context.Background(), s.testCheckOptions(g))
	if err != nil || report.Outcome != autosync.OutcomeNeedsReview || !report.Fetched || spy.fetches() != 1 {
		t.Fatalf("%+v %v fetches=%d", report, err, spy.fetches())
	}
	after := takeAutoSyncSnapshot(t, s, p)
	if after.upstream == before.upstream {
		t.Fatal("check did not fetch")
	}
	after.upstream = before.upstream
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("check wrote state:\n%+v\n%+v", before, after)
	}
	spy.assertNoWrites(t)
	for _, stage := range *p.ledger {
		if stage == "restore-apply" || stage == "push" || stage == "capture" {
			t.Fatalf("check mutated: %v", *p.ledger)
		}
	}
}

func TestAutoCheckUpToDate(t *testing.T) {
	s, _, r, _, _ := executionSyncFixture(t)
	spy := &gitSpyRunner{executionSyncRunner: r}
	s.deps.Runner, s.profileGit.Runner = spy, spy
	g := autoSyncGrant(t, s, autosync.PresetNotifyOnly)
	report, err := s.CheckAutoSync(context.Background(), s.testCheckOptions(g))
	if err != nil || report.Outcome != autosync.OutcomeUpToDate || !report.Fetched || report.Counts.Matching == 0 {
		t.Fatalf("%+v %v", report, err)
	}
	spy.assertNoWrites(t)
}

// Offline and sign-in failures are reported as such, repeat without ever
// escalating to a write, and never mark the computer up to date.
func TestAutoCheckOfflineAndSignInNeverWrite(t *testing.T) {
	s, p, spy, _, g := autoCheckFixture(t)
	before := takeAutoSyncSnapshot(t, s, p)
	for output, want := range map[string]autosync.Outcome{
		"ssh: Could not resolve hostname example.invalid: Name or service not known": autosync.OutcomeOffline,
		"git@example.invalid: Permission denied (publickey).":                        autosync.OutcomeSignIn,
		"fatal: something unexpected":                                                autosync.OutcomeError,
	} {
		spy.fetchOutput = output
		for i := 0; i < 3; i++ {
			report, err := s.CheckAutoSync(context.Background(), s.testCheckOptions(g))
			if err != nil || report.Outcome != want || report.Fetched {
				t.Fatalf("%q: %+v %v", output, report, err)
			}
		}
	}
	spy.assertNoWrites(t)
	if !reflect.DeepEqual(before, takeAutoSyncSnapshot(t, s, p)) {
		t.Fatal("failed checks wrote state")
	}
}

// A fetch that never answers is stopped by the fetch timeout and reported
// as offline; the lock is released.
func TestAutoCheckFetchTimeout(t *testing.T) {
	s, _, spy, _, g := autoCheckFixture(t)
	spy.fetchHangs = true
	opts := s.testCheckOptions(g)
	opts.FetchTimeout = 50 * time.Millisecond
	report, err := s.CheckAutoSync(context.Background(), opts)
	if err != nil || report.Outcome != autosync.OutcomeOffline || report.Reasons[0] != ReasonFetchTimedOut {
		t.Fatalf("%+v %v", report, err)
	}
	state, _ := s.deps.StateHome()
	lock, err := syncengine.AcquireProfileLock(state, s.ProfileDir())
	if err != nil {
		t.Fatal("lock not released after timeout:", err)
	}
	lock.Close()
	// Cancellation of the whole check is returned, not reported.
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	opts.FetchTimeout = time.Minute
	if _, err := s.CheckAutoSync(ctx, opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled check: %v", err)
	}
}

// A profile that is not set up for Sync (no upstream) is reported with the
// inspection's own reason, not as a failed fetch.
func TestAutoCheckProfileWithoutUpstreamNeedsSetup(t *testing.T) {
	s, _, spy, _, g := autoCheckFixture(t)
	syncGit(t, s.ProfileDir(), "branch", "--unset-upstream")
	report, err := s.CheckAutoSync(context.Background(), s.testCheckOptions(g))
	if err != nil || report.Outcome != autosync.OutcomeNeedsReview || report.Fetched || spy.fetches() != 0 || !containsString(report.Reasons, "git-identity") {
		t.Fatalf("%+v %v fetches=%d", report, err, spy.fetches())
	}
}
