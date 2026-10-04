package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/buildinfo"
	"github.com/Grenco/omarchy-blueprint/internal/tui"
	"github.com/Grenco/omarchy-blueprint/internal/updates"
)

type countingSource struct{ calls int }

func (s *countingSource) ID() string { return "counting" }
func (s *countingSource) Latest(context.Context) (updates.Release, error) {
	s.calls++
	v, _ := updates.ParseVersion("99.0.0")
	return updates.Release{Version: v, DetailsURL: "https://github.com/Grenco/omarchy-blueprint/releases/tag/v99.0.0"}, nil
}

// Only the interactive interface checks for updates; CLI commands never do.
func TestCLICommandsNeverCheckForUpdates(t *testing.T) {
	source := &countingSource{}
	state := t.TempDir()
	deps := Dependencies{UpdateSource: source, StateHome: func() (string, error) { return state, nil }, IsTTY: func() bool { return false }}
	for _, args := range [][]string{{"--version"}, {"--help"}, {"--profile", t.TempDir(), "status"}} {
		Execute(context.Background(), args, deps)
	}
	if source.calls != 0 {
		t.Fatalf("CLI commands made %d update requests", source.calls)
	}
	if _, err := os.Stat(updates.CachePath(state)); !os.IsNotExist(err) {
		t.Fatal("a CLI command wrote update state")
	}
}

func TestTUIGetsAPassiveUpdateCheckThatCanBeTurnedOff(t *testing.T) {
	var got tui.Dependencies
	state := t.TempDir()
	source := &countingSource{}
	deps := Dependencies{
		UpdateSource: source,
		StateHome:    func() (string, error) { return state, nil },
		IsTTY:        func() bool { return true },
		RunTUI: func(_ context.Context, _ tui.Options, tuiDeps tui.Dependencies) error {
			got = tuiDeps
			return nil
		},
	}
	if code := Execute(context.Background(), []string{"--profile", filepath.Join(t.TempDir(), ".")}, deps); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if got.CheckForUpdate == nil {
		t.Fatal("the TUI was not given an update check")
	}
	if source.calls != 0 {
		t.Fatal("launching the TUI checked synchronously instead of leaving it to the TUI")
	}
	// A development build has no version to compare, so nothing is fetched.
	if _, ok := got.CheckForUpdate(context.Background()); ok || source.calls != 0 {
		t.Fatalf("a dev build checked for updates: calls=%d", source.calls)
	}
	t.Setenv("OMARCHY_BLUEPRINT_NO_UPDATE_CHECK", "1")
	got = tui.Dependencies{}
	Execute(context.Background(), []string{"--profile", filepath.Join(t.TempDir(), ".")}, deps)
	if got.CheckForUpdate != nil {
		t.Fatal("OMARCHY_BLUEPRINT_NO_UPDATE_CHECK did not turn the check off")
	}
}

func TestTUIUpdateCheckUsesMachineLocalStateForAReleasedBuild(t *testing.T) {
	previous := buildinfo.Version
	buildinfo.Version = "0.1.1"
	t.Cleanup(func() { buildinfo.Version = previous })
	state := t.TempDir()
	source := &countingSource{}
	check := updateCheck(Dependencies{UpdateSource: source, StateHome: func() (string, error) { return state, nil }, Now: time.Now})
	notice, ok := check(context.Background())
	if !ok || notice.Current != "v0.1.1" || notice.Latest != "v99.0.0" {
		t.Fatalf("notice = %+v ok=%t", notice, ok)
	}
	if _, err := os.Stat(updates.CachePath(state)); err != nil {
		t.Fatalf("result was not cached under the state directory: %v", err)
	}
	if check(context.Background()); source.calls != 1 {
		t.Fatalf("a second launch within a day made another request: calls=%d", source.calls)
	}
	failing := updateCheck(Dependencies{UpdateSource: source, StateHome: func() (string, error) { return "", os.ErrPermission }, Now: time.Now})
	if _, ok := failing(context.Background()); ok {
		t.Fatal("an unavailable state directory produced a notice")
	}
}
