package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/autosync"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/gittest"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

var autoSyncCLIHost = strings.Repeat("a", 64)

type cliUserManager struct {
	state autosync.TimerState
	calls []string
}

func (m *cliUserManager) Available(context.Context) error { return nil }
func (m *cliUserManager) DaemonReload(context.Context) error {
	m.calls = append(m.calls, "daemon-reload")
	return nil
}
func (m *cliUserManager) Enable(_ context.Context, u string) error {
	m.calls = append(m.calls, "enable "+u)
	return nil
}
func (m *cliUserManager) Disable(_ context.Context, u string) error {
	m.calls = append(m.calls, "disable "+u)
	return nil
}
func (m *cliUserManager) Stop(_ context.Context, u string) error {
	m.calls = append(m.calls, "stop "+u)
	return nil
}
func (m *cliUserManager) State(context.Context, string) (autosync.TimerState, error) {
	return m.state, nil
}

type cliNotifier struct{ notices []autosync.Notice }

func (n *cliNotifier) Notify(_ context.Context, notice autosync.Notice) error {
	n.notices = append(n.notices, notice)
	return nil
}

type autoSyncCLI struct {
	root, peer string
	deps       Dependencies
	runner     *syncCLIRunner
	manager    *cliUserManager
	notifier   *cliNotifier
	data       profile.Data
}

// autoSyncCLIFixture is a synced profile with a remote (whose URL carries
// a credential that must never be shown) and a peer clone.
func autoSyncCLIFixture(t *testing.T) *autoSyncCLI {
	t.Helper()
	root, deps := configSandbox(t)
	d := profile.New("test", time.Time{})
	d.Manifest.Capture.Packages = true
	d.Packages.Official = []string{"zoxide"}
	d.Machines.Items = []profile.Machine{{Name: "test-machine"}}
	if err := profile.Save(root, d); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", root, "init", "-b", "main")
	gitRun(t, "-C", root, "config", "user.email", "test@example.invalid")
	gitRun(t, "-C", root, "config", "user.name", "Test")
	gitRun(t, "-C", root, "add", ".")
	gitRun(t, "-C", root, "commit", "-m", "base")
	origin := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "init", "--bare", origin)
	gittest.ConfigureRemote(t, origin)
	gitRun(t, "-C", root, "remote", "add", "origin", origin)
	gitRun(t, "-C", root, "push", "-u", "origin", "main")
	info, err := (profilegit.Service{Root: root, Runner: command.SystemRunner{}}).CurrentRevisionInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, _ := deps.StateHome()
	if err := (syncengine.CursorStore{StateHome: state, ProfileRoot: root}).Save(syncengine.Cursor{Version: syncengine.CursorFormatVersion, Revision: info.SHA, Machine: "test-machine", Branch: info.Branch, Upstream: info.Upstream, RemoteFingerprint: info.OriginFingerprint}); err != nil {
		t.Fatal(err)
	}
	if err := (machine.BindingStore{StateHome: state}).Save(root, "test-machine"); err != nil {
		t.Fatal(err)
	}
	peer := filepath.Join(t.TempDir(), "peer")
	gitRun(t, "clone", "-b", "main", origin, peer)
	gitRun(t, "-C", peer, "config", "user.email", "test@example.invalid")
	gitRun(t, "-C", peer, "config", "user.name", "Test")
	f := &autoSyncCLI{root: root, peer: peer, deps: deps, data: d, manager: &cliUserManager{}, notifier: &cliNotifier{}}
	f.runner = &syncCLIRunner{machine: deps.Runner.(*machineRunner)}
	f.deps.Runner = f.runner
	home := t.TempDir()
	f.deps.HomeDir = func() (string, error) { return home, nil }
	f.deps.MiseGlobalConfig = func() (string, error) { return filepath.Join(home, "mise.toml"), nil }
	f.deps.HostID = func() (string, error) { return autoSyncCLIHost, nil }
	f.deps.UserManager = f.manager
	f.deps.Notifier = f.notifier
	config := t.TempDir()
	f.deps.ConfigHome = func() (string, error) { return config, nil }
	return f
}

func (f *autoSyncCLI) remoteChange(t *testing.T) {
	t.Helper()
	d := profile.CloneData(f.data)
	d.Packages.Official = append(d.Packages.Official, "remote")
	if err := profile.Save(f.peer, d); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", f.peer, "add", ".")
	gitRun(t, "-C", f.peer, "commit", "-m", "remote")
	gitRun(t, "-C", f.peer, "push")
}

type yes struct{}

func (yes) Interactive() bool                             { return true }
func (yes) ConfirmAutoSync(autosync.Change) (bool, error) { return true, nil }

// enable stores a confirmed Notify-only grant and selects the profile, as
// PR E's enable command will.
func (f *autoSyncCLI) enable(t *testing.T) {
	t.Helper()
	state, _ := f.deps.StateHome()
	store := autosync.Store{StateHome: state, ProfileRoot: f.root}
	info, _ := (profilegit.Service{Root: f.root, Runner: command.SystemRunner{}}).CurrentRevisionInfo(context.Background())
	next := autosync.DefaultGrant()
	next.Setting, next.Preset = autosync.SettingOn, autosync.PresetNotifyOnly
	next.Identity = autosync.Identity{ProfileRoot: f.root, HostID: autoSyncCLIHost, Machine: "test-machine", Branch: info.Branch, Upstream: info.Upstream, RemoteFingerprint: info.OriginFingerprint}
	approval, err := autosync.ApproveLocally(autosync.DefaultGrant(), next, yes{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(autosync.DefaultGrant(), next, &approval); err != nil {
		t.Fatal(err)
	}
	if err := (autosync.SelectionStore{StateHome: state}).Save(f.root); err != nil {
		t.Fatal(err)
	}
}

func (f *autoSyncCLI) run(t *testing.T, args ...string) (int, string) {
	t.Helper()
	return configRun(t, f.deps, f.root, args...)
}

func stateFiles(t *testing.T, deps Dependencies) []string {
	t.Helper()
	state, _ := deps.StateHome()
	var files []string
	filepath.Walk(state, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, strings.TrimPrefix(path, state))
		}
		return nil
	})
	return files
}

// Status and history never fetch and never create state.
func TestAutoSyncStatusAndHistoryAreReadOnly(t *testing.T) {
	f := autoSyncCLIFixture(t)
	f.remoteChange(t)
	before := stateFiles(t, f.deps)
	code, out := f.run(t, "auto-sync", "status")
	if code != 0 || !strings.Contains(out, "Auto Sync: Off") || !strings.Contains(out, "will not check for or apply shared changes") || !strings.Contains(out, "Last check: never") || !strings.Contains(out, "Last completed Sync:") {
		t.Fatalf("code=%d\n%s", code, out)
	}
	code, out = f.run(t, "auto-sync", "history")
	if code != 0 || !strings.Contains(out, "No checks yet") {
		t.Fatalf("code=%d\n%s", code, out)
	}
	if f.runner.fetches != 0 || strings.Join(before, ",") != strings.Join(stateFiles(t, f.deps), ",") {
		t.Fatalf("status/history fetched (%d) or wrote state", f.runner.fetches)
	}
}

// check fetches once, records the result separately from the completed
// Sync, and never applies; status then shows it.
func TestAutoSyncCheckRecordsAndStatusReports(t *testing.T) {
	f := autoSyncCLIFixture(t)
	f.enable(t)
	f.remoteChange(t)
	head := gitRun(t, "-C", f.root, "rev-parse", "HEAD")
	code, out := f.run(t, "auto-sync", "check")
	if code != 0 || !strings.Contains(out, "Needs review") || !strings.Contains(out, "fetched") || f.runner.fetches != 1 {
		t.Fatalf("code=%d fetches=%d\n%s", code, f.runner.fetches, out)
	}
	if gitRun(t, "-C", f.root, "rev-parse", "HEAD") != head {
		t.Fatal("check moved the checkout")
	}
	if len(f.notifier.notices) != 0 {
		t.Fatal("an explicit check raised a desktop notification")
	}
	code, out = f.run(t, "auto-sync", "status")
	if code != 0 || !strings.Contains(out, "Auto Sync: On — Notify only") || !strings.Contains(out, "Needs review") || !strings.Contains(out, "Last completed Sync:") {
		t.Fatalf("code=%d\n%s", code, out)
	}
	code, out = f.run(t, "--json", "auto-sync", "status")
	var envelope struct {
		Data struct {
			AutoSync struct {
				Setting   string           `json:"setting"`
				Preset    string           `json:"preset"`
				LastCheck *autosync.Report `json:"last_check"`
				Completed *struct {
					Revision string `json:"revision"`
				} `json:"last_completed_sync"`
			} `json:"auto_sync"`
		} `json:"data"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &envelope) != nil {
		t.Fatalf("code=%d\n%s", code, out)
	}
	s := envelope.Data.AutoSync
	if s.Setting != "on" || s.Preset != "notify-only" || s.LastCheck == nil || s.LastCheck.Outcome != autosync.OutcomeNeedsReview || s.Completed == nil || len(s.Completed.Revision) != 40 {
		t.Fatalf("%s", out)
	}
	code, out = f.run(t, "auto-sync", "history")
	if code != 0 || !strings.Contains(out, "Needs review") {
		t.Fatalf("%s", out)
	}
}

// The scheduled entry point uses the selected profile, honours the setting
// and notifies; it ignores --profile.
func TestAutoSyncScheduledCheck(t *testing.T) {
	f := autoSyncCLIFixture(t)
	f.remoteChange(t)
	// Nothing selected: nothing happens.
	if code, _ := configRun(t, f.deps, t.TempDir(), "auto-sync", "scheduled-check"); code != 0 || f.runner.fetches != 0 {
		t.Fatalf("code=%d fetches=%d", code, f.runner.fetches)
	}
	f.enable(t)
	code, out := configRun(t, f.deps, t.TempDir(), "auto-sync", "scheduled-check")
	if code != 0 || f.runner.fetches != 1 || len(f.notifier.notices) != 1 || strings.Contains(out, f.root) {
		t.Fatalf("code=%d fetches=%d notices=%d\n%s", code, f.runner.fetches, len(f.notifier.notices), out)
	}
	// Paused: no fetch.
	state, _ := f.deps.StateHome()
	store := autosync.Store{StateHome: state, ProfileRoot: f.root}
	g, _, _ := store.Load()
	paused := g
	paused.Setting = autosync.SettingPaused
	if _, err := store.Save(g, paused, nil); err != nil {
		t.Fatal(err)
	}
	if code, _ := configRun(t, f.deps, t.TempDir(), "auto-sync", "scheduled-check"); code != 0 || f.runner.fetches != 1 {
		t.Fatalf("paused check fetched: code=%d fetches=%d", code, f.runner.fetches)
	}
	// A live manual review: Busy, no fetch.
	paused.Setting = autosync.SettingOn
	current, _, _ := store.Load()
	approval, _ := autosync.ApproveLocally(current, paused, yes{})
	if _, err := store.Save(current, paused, &approval); err != nil {
		t.Fatal(err)
	}
	lease, err := (autosync.LeaseStore{StateHome: state, ProfileRoot: f.root}).Hold(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	configRun(t, f.deps, t.TempDir(), "auto-sync", "scheduled-check")
	st, _ := (autosync.StatusStore{StateHome: state, ProfileRoot: f.root}).Load()
	if f.runner.fetches != 1 || st.LastCheck.Outcome != autosync.OutcomeBusy {
		t.Fatalf("fetches=%d last=%+v", f.runner.fetches, st.LastCheck)
	}
}

func TestAutoSyncRunIsNotAvailableYet(t *testing.T) {
	f := autoSyncCLIFixture(t)
	f.enable(t)
	f.remoteChange(t)
	code, _ := f.run(t, "auto-sync", "run")
	if code == 0 || f.runner.fetches != 0 {
		t.Fatalf("run: code=%d fetches=%d", code, f.runner.fetches)
	}
	if errOut := f.deps.Err.(interface{ String() string }).String(); !strings.Contains(errOut, "not available yet") {
		t.Fatalf("%s", errOut)
	}
}

func TestAutoSyncPreviewShowsWhatWouldHappen(t *testing.T) {
	f := autoSyncCLIFixture(t)
	f.enable(t)
	f.remoteChange(t)
	code, out := f.run(t, "auto-sync", "preview")
	if code != 0 || !strings.Contains(out, "What Auto Sync would do") || !strings.Contains(out, "not available yet") || f.runner.fetches != 1 {
		t.Fatalf("code=%d fetches=%d\n%s", code, f.runner.fetches, out)
	}
	code, out = f.run(t, "--json", "auto-sync", "preview", "--no-fetch")
	if code != 0 || f.runner.fetches != 1 || !strings.Contains(out, `"fetched": false`) {
		t.Fatalf("code=%d\n%s", code, out)
	}
}

// Nothing Auto Sync prints carries the remote URL or its credential.
func TestAutoSyncOutputNeverShowsTheRemote(t *testing.T) {
	f := autoSyncCLIFixture(t)
	f.enable(t)
	// The configured URL carries a credential; insteadOf keeps the test
	// offline by mapping it to the local origin.
	origin := gitRun(t, "-C", f.root, "remote", "get-url", "origin")
	gitRun(t, "-C", f.root, "config", "url."+strings.TrimSpace(origin)+".insteadOf", "https://user:s3cret@example.invalid/profile.git")
	gitRun(t, "-C", f.root, "remote", "set-url", "origin", "https://user:s3cret@example.invalid/profile.git")
	for _, args := range [][]string{{"auto-sync", "check"}, {"auto-sync", "status"}, {"--json", "auto-sync", "status"}, {"auto-sync", "history"}, {"--json", "auto-sync", "history"}} {
		_, out := f.run(t, args...)
		errOut := f.deps.Err.(interface{ String() string }).String()
		if strings.Contains(out+errOut, "s3cret") || strings.Contains(out+errOut, "example.invalid") {
			t.Fatalf("%v leaked the remote:\n%s%s", args, out, errOut)
		}
	}
}

// A profile not set up for Sync says what to do, without fetching.
func TestAutoSyncCheckExplainsAProfileNotSetUpForSync(t *testing.T) {
	root, deps := configSandbox(t)
	deps.HostID = func() (string, error) { return autoSyncCLIHost, nil }
	deps.UserManager, deps.Notifier = &cliUserManager{}, &cliNotifier{}
	config := t.TempDir()
	deps.ConfigHome = func() (string, error) { return config, nil }
	code, out := configRun(t, deps, root, "auto-sync", "check")
	if code != 0 || !strings.Contains(out, "Needs review") || !strings.Contains(out, "Choose which machine this computer is") || strings.Contains(out, "could not finish") {
		t.Fatalf("code=%d\n%s", code, out)
	}
}
