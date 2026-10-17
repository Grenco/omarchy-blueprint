package autosync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type controlFixture struct {
	storeFixture
	control Control
	manager *fakeManager
	units   string
}

func newControl(t *testing.T) controlFixture {
	t.Helper()
	f := newStore(t)
	scheduler, m, units := schedulerFixture(t)
	return controlFixture{storeFixture: f, manager: m, units: units, control: Control{
		Store:     f.store,
		Selection: SelectionStore{StateHome: f.store.StateHome},
		Scheduler: scheduler,
		Units:     unitConfig,
	}}
}

func (f controlFixture) approve(base, next Grant) *LocalApproval {
	f.t.Helper()
	approval, err := ApproveLocally(base, next, &confirmer{interactive: true, answer: true})
	if err != nil {
		f.t.Fatal(err)
	}
	return &approval
}

func TestEnableWritesGrantSelectsProfileAndSchedules(t *testing.T) {
	f := newControl(t)
	next := f.proposal(PresetNotifyOnly)
	saved, err := f.control.Enable(context.Background(), DefaultGrant(), next, f.approve(DefaultGrant(), next))
	if err != nil || saved.Setting != SettingOn || !f.manager.enabled {
		t.Fatalf("%+v %v %v", saved, err, f.manager.calls)
	}
	selected, ok, err := f.control.Selection.Load()
	if err != nil || !ok || selected != f.root {
		t.Fatalf("selection %q %v %v", selected, ok, err)
	}
	// Without a confirmation nothing is written and nothing is scheduled.
	g := newControl(t)
	if _, err := g.control.Enable(context.Background(), DefaultGrant(), g.proposal(PresetNotifyOnly), nil); !errors.Is(err, ErrApprovalRequired) {
		t.Fatal(err)
	}
	if g.manager.enabled || len(g.manager.calls) != 0 {
		t.Fatal("scheduled without a confirmed grant")
	}
}

// If scheduling fails, the grant is turned back off and the previous
// profile selection restored: no half-enabled state.
func TestEnableRollsBackWhenSchedulingFails(t *testing.T) {
	f := newControl(t)
	f.manager.failOn = "enable " + TimerUnit
	next := f.proposal(PresetNotifyOnly)
	if _, err := f.control.Enable(context.Background(), DefaultGrant(), next, f.approve(DefaultGrant(), next)); err == nil {
		t.Fatal("enable succeeded")
	}
	g, _, err := f.store.Load()
	if err != nil || g.Setting != SettingOff {
		t.Fatalf("grant left on: %+v %v", g, err)
	}
	if _, ok, _ := f.control.Selection.Load(); ok {
		t.Fatal("selection left behind")
	}
	if _, err := os.Stat(filepath.Join(f.units, TimerUnit)); !os.IsNotExist(err) {
		t.Fatal("timer left behind")
	}
}

// Disable revokes permission before it touches the timer, so a timer that
// cannot be disabled still finds Auto Sync off.
func TestDisableRevokesBeforeTheTimer(t *testing.T) {
	f := newControl(t)
	next := f.proposal(PresetNotifyOnly)
	saved, _ := f.control.Enable(context.Background(), DefaultGrant(), next, f.approve(DefaultGrant(), next))
	f.manager.failOn = "disable " + TimerUnit
	if _, err := f.control.Disable(context.Background(), saved); err == nil {
		t.Fatal("disable reported success with the timer still enabled")
	}
	if g, _, _ := f.store.Load(); g.Setting != SettingOff {
		t.Fatalf("permission not revoked first: %+v", g)
	}
	f.manager.failOn = ""
	current, _, _ := f.store.Load()
	if _, err := f.control.Disable(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if f.manager.enabled {
		t.Fatal("timer still enabled")
	}
	if _, ok, _ := f.control.Selection.Load(); ok {
		t.Fatal("selection kept after disable")
	}
}

// Pause needs no confirmation, takes effect in the grant at once and stops
// the timer; resuming the same authority needs none either, and a timer
// error never undoes the pause.
func TestPauseAndResume(t *testing.T) {
	f := newControl(t)
	next := f.proposal(PresetNotifyOnly)
	saved, _ := f.control.Enable(context.Background(), DefaultGrant(), next, f.approve(DefaultGrant(), next))
	f.manager.calls = nil
	f.manager.failOn = "stop " + TimerUnit
	paused, err := f.control.Pause(context.Background(), saved)
	if err != nil || paused.Setting != SettingPaused {
		t.Fatalf("%+v %v", paused, err)
	}
	if g, _, _ := f.store.Load(); g.Setting != SettingPaused {
		t.Fatal("pause not stored")
	}
	f.manager.failOn = ""
	resumed, err := f.control.Resume(context.Background(), paused, nil)
	if err != nil || resumed.Setting != SettingOn || !f.manager.enabled {
		t.Fatalf("%+v %v", resumed, err)
	}
	// A resume that cannot schedule stays paused.
	paused, _ = f.control.Pause(context.Background(), resumed)
	f.manager.failOn = "enable " + TimerUnit
	if _, err := f.control.Resume(context.Background(), paused, nil); err == nil {
		t.Fatal("resume succeeded")
	}
	if g, _, _ := f.store.Load(); g.Setting != SettingPaused {
		t.Fatalf("failed resume left %s", g.Setting)
	}
	f.manager.failOn = ""
	resumed, _ = f.control.Resume(context.Background(), mustLoad(t, f.store), nil)
	// Resuming with wider authority needs a confirmation.
	paused, _ = f.control.Pause(context.Background(), resumed)
	wider := paused
	wider.Preset = PresetConservative
	wider.Categories = PresetScope([]string{"themes"})
	if _, err := f.control.ResumeWith(context.Background(), paused, wider, nil); !errors.Is(err, ErrApprovalRequired) {
		t.Fatal(err)
	}
}

// Enabling another profile moves the single timer to it.
func TestOneSelectedProfilePerTimer(t *testing.T) {
	f := newControl(t)
	next := f.proposal(PresetNotifyOnly)
	f.control.Enable(context.Background(), DefaultGrant(), next, f.approve(DefaultGrant(), next))
	other := newStore(t)
	other.store.StateHome = f.store.StateHome
	otherControl := f.control
	otherControl.Store = other.store
	otherNext := other.proposal(PresetNotifyOnly)
	if _, err := otherControl.Enable(context.Background(), DefaultGrant(), otherNext, f.approve(DefaultGrant(), otherNext)); err != nil {
		t.Fatal(err)
	}
	selected, _, _ := f.control.Selection.Load()
	if selected != other.root {
		t.Fatalf("selected %q", selected)
	}
	// Disabling the profile that is no longer selected leaves the timer.
	current, _, _ := f.store.Load()
	f.manager.calls = nil
	if _, err := f.control.Disable(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if !f.manager.enabled || strings.Contains(strings.Join(f.manager.calls, ","), "disable") {
		t.Fatalf("disabled another profile's timer: %v", f.manager.calls)
	}
}

func TestSelectionStoreIsPrivateAndFailsClosed(t *testing.T) {
	f := newStore(t)
	s := SelectionStore{StateHome: f.store.StateHome}
	if _, ok, err := s.Load(); ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := s.Save(f.root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.store.StateHome, "omarchy-blueprint", selectionFile)
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal(info.Mode())
	}
	os.Chmod(path, 0o644)
	if _, _, err := s.Load(); err == nil {
		t.Fatal("readable selection trusted")
	}
	os.WriteFile(path, []byte(`{"version":1,"profile_root":"relative"}`), 0o600)
	os.Chmod(path, 0o600)
	if _, _, err := s.Load(); err == nil {
		t.Fatal("relative profile root accepted")
	}
}

func mustLoad(t *testing.T, s Store) Grant {
	t.Helper()
	g, _, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	return g
}
