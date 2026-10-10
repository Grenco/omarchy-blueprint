package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
	"github.com/Grenco/omarchy-blueprint/internal/updates"
)

var newerRelease = updates.Notice{Current: "v0.1.1", Latest: "v0.1.2", DetailsURL: "https://github.com/Grenco/omarchy-blueprint/releases/tag/v0.1.2", Instructions: "Download this release's PKGBUILD."}

// modelWithOverview is a model with a real (sessionless) Overview screen;
// without a session the registry only installs placeholders.
func modelWithOverview() model {
	m := newModel(ThemeLoader{NoColor: true})
	m.screens[ScreenOverview] = &overviewScreen{Overview: screens.NewOverview(nil)}
	return m
}

func overviewView(t *testing.T, m model) string {
	t.Helper()
	overview, ok := m.screens[ScreenOverview].(*overviewScreen)
	if !ok {
		t.Fatal("no Overview screen")
	}
	return overview.View()
}

// A check that never answers must not hold up Init, the first frame or
// interaction: it only runs when Bubble Tea executes the returned command,
// off the event loop.
func TestUpdateCheckNeverBlocksStartup(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	m := modelWithOverview()
	m.updates = &updateTracker{check: func(ctx context.Context) (updates.Notice, bool) {
		calls.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return newerRelease, true
	}}
	started := time.Now()
	if m.Init() == nil {
		t.Fatal("Init returned nothing")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond || calls.Load() != 0 {
		t.Fatalf("Init ran the check itself (took %s, calls=%d)", elapsed, calls.Load())
	}
	if frame := m.View(); strings.Contains(frame.Content, "Update available") {
		t.Fatal("first frame waited for or invented an update result")
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	m = updated.(model)
	if m.Init(); m.updates.requestID != 1 {
		t.Fatalf("a second Init started another check (request %d)", m.updates.requestID)
	}
	// Run the check the way Bubble Tea would: in the background.
	result := make(chan tea.Msg, 1)
	m.updates.started = false
	m.updates.requestID = 0
	cmd := m.startUpdateCheck()
	go func() { result <- cmd() }()
	close(release)
	updated, _ = m.Update(<-result)
	if view := overviewView(t, updated.(model)); !strings.Contains(view, "Update available · v0.1.1 → v0.1.2") {
		t.Fatalf("result was not shown on Overview:\n%s", view)
	}
}

func TestUpdateNoticeOnlyForANewerRelease(t *testing.T) {
	m := modelWithOverview()
	m.updates = &updateTracker{started: true, requestID: 1}
	updated, _ := m.Update(updateCheckedMsg{requestID: 1, ok: false})
	if view := overviewView(t, updated.(model)); strings.Contains(view, "Update available") {
		t.Fatalf("no newer release (or a silent failure) still showed a notice:\n%s", view)
	}
}

func TestStaleUpdateResultsChangeNothing(t *testing.T) {
	m := modelWithOverview()
	m.updates = &updateTracker{started: true, requestID: 2}
	m.selected, m.notification = 3, "Profile saved."
	updated, cmd := m.Update(updateCheckedMsg{requestID: 1, notice: newerRelease, ok: true})
	after := updated.(model)
	if cmd != nil || after.updates.notice != nil || after.selected != 3 || after.notification != "Profile saved." {
		t.Fatalf("a stale result changed state: cmd=%v notice=%v selected=%d notification=%q", cmd != nil, after.updates.notice, after.selected, after.notification)
	}
	if view := overviewView(t, after); strings.Contains(view, "Update available") {
		t.Fatal("a stale result reached Overview")
	}
}

// Creating a profile from the welcome flow rebuilds the model. A check still
// in flight belongs to the launch, not the old model: it lands in the new
// one, which must not start a second check, and it touches nothing but the
// notice.
func TestUpdateResultSurvivesARebuiltModelWithoutTouchingSession(t *testing.T) {
	old := modelWithOverview()
	old.updates = &updateTracker{check: func(context.Context) (updates.Notice, bool) { return newerRelease, true }}
	cmd := old.startUpdateCheck()
	fresh := modelWithOverview()
	fresh.updates = old.updates
	if fresh.startUpdateCheck() != nil {
		t.Fatal("the rebuilt model started a second check")
	}
	session, selected, focus := fresh.session, fresh.selected, fresh.focus
	updated, _ := fresh.Update(cmd())
	after := updated.(model)
	if after.session != session || after.selected != selected || after.focus != focus {
		t.Fatal("an update result changed session or navigation state")
	}
	if view := overviewView(t, after); !strings.Contains(view, "v0.1.1 → v0.1.2") {
		t.Fatalf("result did not land in the rebuilt model:\n%s", view)
	}
	again := modelWithOverview()
	again.updates = after.updates
	again.applyUpdateNotice()
	if view := overviewView(t, again); !strings.Contains(view, "v0.1.2") {
		t.Fatal("a model rebuilt after the result lost the notice")
	}
}

func TestNoUpdateCheckWithoutAChecker(t *testing.T) {
	m := modelWithOverview()
	if m.startUpdateCheck() != nil {
		t.Fatal("a model without a checker started one")
	}
	m.updates = &updateTracker{}
	if m.startUpdateCheck() != nil {
		t.Fatal("an opted-out launch started a check")
	}
}
