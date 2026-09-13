package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestTUIModelRendersTitleInAlternateScreen(t *testing.T) {
	view := (model{}).View()
	if view.Content != "Omarchy Blueprint" {
		t.Errorf("content = %q", view.Content)
	}
	if !view.AltScreen {
		t.Error("alternate screen is disabled")
	}
}

func TestTUIModelQuitsOnQAndCtrlC(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 'q'}, {Code: 'c', Mod: tea.ModCtrl}} {
		_, cmd := (model{}).Update(key)
		if cmd == nil || cmd() != tea.Quit() {
			t.Errorf("key %q did not quit", key.String())
		}
	}
}

func TestWelcomeCreatesProfileAndTransitionsToSession(t *testing.T) {
	dir := t.TempDir()
	created := false
	m := newModelWithContext(context.Background(), func() {}, ThemeLoader{}, nil, dir, func(_ context.Context, gotDir, name string) (*workflow.Session, error) {
		created = true
		if gotDir != dir || name != filepath.Base(dir) {
			t.Fatalf("dir=%q name=%q", gotDir, name)
		}
		return nil, nil
	})
	if m.modal != modalWelcome || m.profileDir != dir || m.welcomeName.Value() != filepath.Base(dir) || !strings.Contains(m.modalView("", layout{workspaceWidth: 80, contentHeight: 20}), "Create a new profile at:") {
		t.Fatalf("welcome=%#v", m)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("create command is missing")
	}
	msg := cmd()
	if _, ok := msg.(profileCreatedMsg); !ok || !created {
		t.Fatalf("create result=%#v created=%t", msg, created)
	}
}

func TestWelcomeChooserCanOpenCreateAndQuit(t *testing.T) {
	dir := t.TempDir()
	opened, created := "", ""
	m := newModelWithContext(context.Background(), func() {}, ThemeLoader{NoColor: true}, nil, dir, func(_ context.Context, path, _ string) (*workflow.Session, error) { created = path; return nil, nil })
	m.enableProfileChooser(func(options workflow.Options) (*workflow.Session, error) {
		opened = options.ProfileDir
		return nil, nil
	}, "desktop")
	if view := m.modalView("", layout{workspaceWidth: 80, contentHeight: 20}); !strings.Contains(view, "Create a new profile") || !strings.Contains(view, "Open an existing profile") || !strings.Contains(view, "Quit") {
		t.Fatalf("chooser = %q", view)
	}
	m.welcomeChoice = 1
	m, _ = updateWelcomeModel(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.welcomePath.Input.SetValue(dir)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("open command missing")
	}
	_ = cmd()
	if opened != dir {
		t.Fatalf("opened = %q", opened)
	}

	m.welcomeBusy, m.welcomeStep, m.welcomeChoice = false, "choose", 0
	m, _ = updateWelcomeModel(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	createDir := filepath.Join(dir, "new-profile")
	m.welcomePath.Input.SetValue(createDir)
	m, _ = updateWelcomeModel(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.welcomeName.Input.SetValue("new")
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("create command missing")
	}
	_ = cmd()
	if created != createDir {
		t.Fatalf("created = %q", created)
	}

	m.welcomeBusy, m.welcomeStep, m.welcomeChoice = false, "choose", 2
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || cmd() != tea.Quit() {
		t.Fatal("quit command missing")
	}
}

func updateWelcomeModel(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(model), cmd
}

func TestRunDoesNotOnboardCorruptProfile(t *testing.T) {
	dir := t.TempDir()
	created := false
	err := Run(context.Background(), Options{ProfileDir: dir}, Dependencies{
		OpenSession: func(workflow.Options) (*workflow.Session, error) {
			return nil, errors.New("parse profile.toml: invalid")
		},
		CreateProfile: func(context.Context, string, string) (*workflow.Session, error) { created = true; return nil, nil },
	})
	if err == nil || created {
		t.Fatalf("err=%v created=%t", err, created)
	}
	if _, err := os.Stat(filepath.Join(dir, "profile.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile created: %v", err)
	}
}

func TestRunDoesNotOnboardExplicitMissingProfile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	created := false
	want := &os.PathError{Op: "open", Path: filepath.Join(dir, "profile.toml"), Err: os.ErrNotExist}
	err := Run(context.Background(), Options{ProfileDir: dir, ProfileExplicit: true}, Dependencies{
		OpenSession:   func(workflow.Options) (*workflow.Session, error) { return nil, want },
		CreateProfile: func(context.Context, string, string) (*workflow.Session, error) { created = true; return nil, nil },
	})
	if !errors.Is(err, os.ErrNotExist) || created {
		t.Fatalf("err=%v created=%t", err, created)
	}
}

func TestRunDoesNotOnboardImplicitPartialProfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "unrelated"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := &os.PathError{Op: "open", Path: filepath.Join(dir, "profile.toml"), Err: os.ErrNotExist}
	err := Run(context.Background(), Options{ProfileDir: dir}, Dependencies{OpenSession: func(workflow.Options) (*workflow.Session, error) { return nil, want }})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err=%v", err)
	}
}
