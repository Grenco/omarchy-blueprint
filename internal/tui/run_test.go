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
	if m.modal != modalWelcome || m.profileDir != dir || m.welcomeName.Value() != filepath.Base(dir) || !strings.Contains(m.modalView("", layout{workspaceWidth: 80, contentHeight: 20}), "No profile exists at:") {
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
