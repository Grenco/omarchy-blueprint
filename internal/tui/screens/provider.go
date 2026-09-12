package screens

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Provider presents status and capture for providers without per-item TUI
// policy. Domain-specific screens retain their typed interactions elsewhere.
type Provider struct {
	session                 *workflow.Session
	id                      string
	width, height, selected int
	status                  workflow.ProviderStatus
	err                     error
}

type providerStatusMsg struct {
	status workflow.ProviderStatus
	err    error
}
type providerCaptureMsg struct{ err error }

// CaptureComplete tells the root model to refresh dependent local views.
type CaptureComplete struct {
	Provider string
	Err      error
}

func NewProvider(session *workflow.Session, id string) *Provider {
	return &Provider{session: session, id: id}
}
func (s *Provider) Refresh() tea.Cmd          { return s.refresh() }
func (s *Provider) SetSize(width, height int) { s.width, s.height = width, height }
func (s *Provider) Init() tea.Cmd             { return s.refresh() }

func (s *Provider) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case providerStatusMsg:
		s.status, s.err = msg.status, msg.err
		if s.selected >= len(s.status.Changes) {
			s.selected = max(0, len(s.status.Changes)-1)
		}
		return nil
	case providerCaptureMsg:
		s.err = msg.err
		if msg.err == nil {
			return s.refresh()
		}
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		if s.selected < len(s.status.Changes)-1 {
			s.selected++
		}
	case "k", "up":
		if s.selected > 0 {
			s.selected--
		}
	case "c":
		return s.capture()
	}
	return nil
}

func (s *Provider) View() string {
	title := strings.ToUpper(s.id[:1]) + s.id[1:]
	if s.err != nil {
		return title + "\n\nUnable to load " + title + ": " + s.err.Error()
	}
	lines := []string{title}
	if !s.status.Captured {
		lines = append(lines, "Not captured. Press c to capture.")
	}
	for i, change := range s.status.Changes {
		marker := " "
		if i == s.selected {
			marker = ">"
		}
		lines = append(lines, marker+" "+change.Summary)
	}
	if len(s.status.Changes) == 0 && s.status.Captured {
		lines = append(lines, "No differences.")
	}
	lines = append(lines, "c capture  j/k select")
	return strings.Join(lines, "\n")
}

func (s *Provider) refresh() tea.Cmd {
	return func() tea.Msg {
		report, err := s.session.Status(context.Background(), s.id)
		if err != nil && !providerCaptured(s.session.Profile(), s.id) {
			return providerStatusMsg{status: workflow.ProviderStatus{ID: s.id}}
		}
		if err != nil {
			return providerStatusMsg{err: err}
		}
		for _, status := range report.Providers {
			if status.ID == s.id {
				return providerStatusMsg{status: status}
			}
		}
		return providerStatusMsg{err: fmt.Errorf("%s status is unavailable", s.id)}
	}
}
func (s *Provider) capture() tea.Cmd {
	return func() tea.Msg {
		_, err := s.session.Capture(context.Background(), s.id)
		return CaptureComplete{Provider: s.id, Err: err}
	}
}

func providerCaptured(data profile.Data, id string) bool {
	switch id {
	case "packages":
		return true
	case "themes":
		return data.Manifest.Capture.Themes
	case "plugins":
		return data.Manifest.Capture.Plugins
	case "shell":
		return data.Manifest.Capture.Shell
	case "hooks":
		return data.Manifest.Capture.Hooks
	case "defaults":
		return data.Manifest.Capture.Defaults
	default:
		return false
	}
}
