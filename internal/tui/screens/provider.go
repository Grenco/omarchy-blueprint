package screens

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Provider presents status and capture for providers without per-item TUI
// policy. Domain-specific screens retain their typed interactions elsewhere.
type Provider struct {
	ctx                     context.Context
	session                 *workflow.Session
	id                      string
	width, height, selected int
	status                  workflow.ProviderStatus
	list                    components.Selectable
	styles                  components.Styles
	busy, confirm           bool
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
	return NewProviderContext(context.Background(), session, id)
}
func NewProviderContext(ctx context.Context, session *workflow.Session, id string) *Provider {
	return &Provider{ctx: ctx, session: session, id: id}
}
func (s *Provider) Refresh() tea.Cmd                   { return s.refresh() }
func (s *Provider) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Provider) SetSize(width, height int)          { s.width, s.height = width, height }
func (s *Provider) Init() tea.Cmd                      { return s.refresh() }

func (s *Provider) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case providerStatusMsg:
		s.status, s.err, s.busy = msg.status, msg.err, false
		if s.selected >= len(s.status.Changes) {
			s.selected = max(0, len(s.status.Changes)-1)
		}
		return nil
	case providerCaptureMsg:
		s.err, s.busy = msg.err, false
		if msg.err == nil {
			return func() tea.Msg { return CaptureComplete{Provider: s.id} }
		}
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "r":
		if !s.busy {
			return s.refresh()
		}
	case "j", "down":
		s.list.Move(1, len(s.status.Changes), s.listHeight())
		s.selected = s.list.Selected
	case "k", "up":
		s.list.Move(-1, len(s.status.Changes), s.listHeight())
		s.selected = s.list.Selected
	case "c":
		if !s.busy {
			s.confirm = true
			return func() tea.Msg {
				return components.ModalRequest{Title: "Capture " + titleFor(s.id), Content: components.Confirm("Capture observed changes into the profile?")}
			}
		}
	case "enter":
		if s.confirm {
			s.confirm, s.busy = false, true
			return s.capture()
		}
	case "esc":
		s.confirm = false
	}
	return nil
}

func (s *Provider) View() string {
	title := titleFor(s.id)
	if s.err != nil {
		return s.styles.Error(title + "\n\n! Unable to load " + title + ": " + s.err.Error())
	}
	lines := []string{title}
	if s.busy {
		lines = append(lines, "Loading...")
	}
	if !s.status.Captured {
		lines = append(lines, "Not captured. Press c to capture.")
	}
	rows := make([]string, 0, len(s.status.Changes))
	for i, change := range s.status.Changes {
		marker := " "
		if i == s.selected {
			marker = ">"
		}
		rows = append(rows, marker+" "+change.Summary)
	}
	width := s.width
	if width == 0 {
		width = 120
	}
	lines = append(lines, s.list.View(rows, width, s.listHeight()))
	if len(s.status.Changes) == 0 && s.status.Captured {
		lines = append(lines, "No differences.", "", "Saved snapshot:")
		lines = append(lines, snapshotLines(s.status.Snapshot)...)
	}
	lines = append(lines, "Capture: c capture")
	return strings.Join(lines, "\n")
}

func (s *Provider) refresh() tea.Cmd {
	s.busy = true
	return func() tea.Msg {
		report, err := s.session.Status(s.ctx, s.id)
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

func snapshotLines(snapshot any) []string {
	switch value := snapshot.(type) {
	case profile.Packages:
		return []string{fmt.Sprintf("  official: %d  aur: %d  mise: %d", len(value.Official), len(value.AUR), len(value.Mise)), fmt.Sprintf("  machine-specific: %d  excluded: %d", len(value.MachineSpecific), len(value.Excluded))}
	case profile.Themes:
		return []string{fmt.Sprintf("  current: %s", value.Current), fmt.Sprintf("  saved themes: %d", len(value.Items))}
	case profile.Plugins:
		return []string{fmt.Sprintf("  saved plugins: %d", len(value.Items))}
	case profile.Defaults:
		return []string{"  terminal: " + empty(value.Terminal), "  browser: " + empty(value.Browser), "  editor: " + empty(value.Editor), "  agent: " + empty(value.Agent)}
	case profile.Shell:
		return []string{fmt.Sprintf("  format: %d", value.Version), "  snapshot: " + empty(value.Hash), "  baseline: " + empty(value.BaselineHash)}
	case profile.Hooks:
		return []string{fmt.Sprintf("  saved hooks: %d", len(value.Items))}
	default:
		return []string{"  (empty)"}
	}
}
func (s *Provider) capture() tea.Cmd {
	return func() tea.Msg {
		_, err := s.session.Capture(s.ctx, s.id)
		return providerCaptureMsg{err: err}
	}
}

func (s *Provider) DetailView() string {
	title := titleFor(s.id)
	if s.err != nil {
		return title + " details unavailable: " + s.err.Error()
	}
	if change := s.selectedChange(); change.Summary != "" {
		return title + " change\n" + change.Summary
	}
	lines := []string{title + " snapshot"}
	return strings.Join(append(lines, snapshotLines(s.status.Snapshot)...), "\n")
}
func (s *Provider) selectedChange() model.Change {
	if s.list.Selected >= 0 && s.list.Selected < len(s.status.Changes) {
		return s.status.Changes[s.list.Selected]
	}
	return model.Change{}
}
func (s *Provider) listHeight() int {
	if s.height == 0 {
		return len(s.status.Changes) + 1
	}
	return max(1, s.height-5)
}
func titleFor(id string) string {
	if id == "" {
		return "Provider"
	}
	return strings.ToUpper(id[:1]) + id[1:]
}
func empty(value string) string {
	if value == "" {
		return "(unset)"
	}
	return value
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
