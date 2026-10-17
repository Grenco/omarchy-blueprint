package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type Options struct {
	ProfileDir      string
	ProfileExplicit bool
	Machine         string
}

type Dependencies struct {
	Workflow      workflow.Dependencies
	OpenSession   func(workflow.Options) (*workflow.Session, error)
	CreateProfile func(context.Context, string, string) (*workflow.Session, error)
	// CheckForUpdate, when set, runs once in the background per launch.
	CheckForUpdate UpdateCheck
}

func Run(ctx context.Context, options Options, deps Dependencies) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	loader := ThemeLoader{StateHome: deps.Workflow.StateHome, NoColor: os.Getenv("NO_COLOR") != ""}
	var session *workflow.Session
	chooser := false
	var err error
	if deps.OpenSession != nil {
		session, err = deps.OpenSession(workflow.Options{ProfileDir: options.ProfileDir, ExplicitMachine: options.Machine})
		if err != nil {
			if options.ProfileExplicit {
				return err
			} else if profileChooserNeeded(options.ProfileDir, err) {
				chooser = true
			} else {
				return err
			}
		}
	}
	m := newModelWithContext(ctx, cancel, loader, session, options.ProfileDir, deps.CreateProfile)
	if chooser {
		m.enableProfileChooser(deps.OpenSession, options.Machine)
	}
	m.updates = &updateTracker{check: deps.CheckForUpdate}
	final, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	// An open Sync review's Auto Sync lease ends with the TUI (it would
	// lapse on its own, but scheduled checks need not wait).
	releaseSyncReview(session)
	if fm, ok := final.(model); ok && fm.session != session {
		releaseSyncReview(fm.session)
	}
	return err
}

func releaseSyncReview(session *workflow.Session) {
	if session != nil {
		_ = session.ReleaseSyncReview()
	}
}

func profileChooserNeeded(dir string, openErr error) bool {
	if !errors.Is(openErr, os.ErrNotExist) {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "profile.toml"))
	return errors.Is(err, os.ErrNotExist)
}

func themeTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return themeTickMsg{} })
}
