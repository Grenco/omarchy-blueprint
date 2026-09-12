package tui

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type Options struct {
	ProfileDir string
	Machine    string
}

type Dependencies struct {
	Workflow    workflow.Dependencies
	OpenSession func(workflow.Options) (*workflow.Session, error)
}

func Run(ctx context.Context, options Options, deps Dependencies) error {
	loader := ThemeLoader{StateHome: deps.Workflow.StateHome, NoColor: os.Getenv("NO_COLOR") != ""}
	var session *workflow.Session
	var err error
	if deps.OpenSession != nil {
		session, err = deps.OpenSession(workflow.Options{ProfileDir: options.ProfileDir, ExplicitMachine: options.Machine})
		if err != nil {
			return err
		}
	}
	_, err = tea.NewProgram(newModelWithSession(loader, session), tea.WithContext(ctx)).Run()
	return err
}

func themeTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return themeTickMsg{} })
}
