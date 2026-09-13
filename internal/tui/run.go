package tui

import (
	"context"
	"os"
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
			} else if workflow.IsMissingProfile(options.ProfileDir, err) {
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
	_, err = tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}

func themeTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return themeTickMsg{} })
}
