package screens

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Capture composes existing provider capture operations into one discoverable screen.
type Capture struct {
	ctx                                  context.Context
	session                              *workflow.Session
	statuses                             []workflow.ProviderStatus
	selected, cursor                     int
	height                               int
	chosen                               map[string]bool
	table                                components.Table
	navigation                           components.Selectable
	styles                               components.Styles
	confirm, busy, capturing, captureAll bool
	err                                  error
}

type captureStatusMsg struct {
	statuses []workflow.ProviderStatus
	err      error
}
type captureDoneMsg struct{ err error }

func NewCaptureContext(ctx context.Context, session *workflow.Session) *Capture {
	return &Capture{ctx: ctx, session: session, chosen: map[string]bool{}}
}
func (s *Capture) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Capture) SetSize(_, height int)              { s.height = height }
func (s *Capture) Init() tea.Cmd                      { return s.refresh() }
func (s *Capture) TransientActive() bool              { return s.confirm || s.busy }
func (s *Capture) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case captureStatusMsg:
		s.statuses, s.err, s.busy = msg.statuses, msg.err, false
		return nil
	case captureDoneMsg:
		s.err, s.busy, s.confirm, s.capturing, s.captureAll = msg.err, false, false, false, false
		return s.refresh()
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || s.busy {
		return nil
	}
	if s.confirm {
		if key.String() == "esc" {
			s.confirm, s.captureAll = false, false
			return nil
		}
		if key.String() == "enter" {
			s.busy, s.confirm, s.capturing = true, false, true
			return s.capture()
		}
		return nil
	}
	s.navigation.Selected = s.cursor
	if s.navigation.Vim(key.String(), len(s.statuses), max(1, s.height-2)) {
		s.cursor = s.navigation.Selected
		return nil
	}
	switch key.String() {
	case "j", "down":
		s.cursor = min(len(s.statuses)-1, s.cursor+1)
	case "k", "up":
		s.cursor = max(0, s.cursor-1)
	case "space", " ":
		if p := s.current(); p.ID != "" {
			s.chosen[p.ID] = !s.chosen[p.ID]
		}
	case "a":
		for _, p := range s.statuses {
			if len(p.Changes) > 0 {
				s.chosen[p.ID] = true
			}
		}
	case "c":
		s.captureAll = false
		return s.confirmCapture()
	case "C", "shift+c":
		s.captureAll = true
		for _, p := range s.statuses {
			s.chosen[p.ID] = true
		}
		return s.confirmCapture()
	case "r":
		return s.refresh()
	}
	return nil
}
func (s *Capture) View() string {
	if s.err != nil {
		return s.styles.Error("Capture failed: " + s.err.Error())
	}
	if s.confirm {
		return "Capture selected providers?\n\nEnter confirms. Escape cancels."
	}
	if s.capturing {
		return "Capturing selected providers...\n\nThis can take a while while providers inspect the running system."
	}
	if s.busy {
		return "Loading capture status..."
	}
	rows := make([]components.Row, 0, len(s.statuses))
	for i, p := range s.statuses {
		changes := "clean"
		if len(p.Changes) > 0 {
			changes = fmt.Sprintf("%d", len(p.Changes))
		}
		check := " "
		if s.chosen[p.ID] {
			check = "x"
		}
		rows = append(rows, components.Row{Cells: []string{"[" + check + "]", p.ID, changes}, Selected: i == s.cursor, Focused: true})
	}
	return s.table.Render([]components.Column{{Title: "", MinWidth: 3}, {Title: "Provider", MinWidth: 12}, {Title: "Changes", MinWidth: 8}}, rows, 60, max(2, len(rows)+1), s.styles)
}
func (s *Capture) DetailView() string {
	p := s.current()
	if p.ID == "" {
		return "Select providers to capture."
	}
	return "Provider: " + p.ID + "\nChanges: " + fmt.Sprint(len(p.Changes)) + "\nSelected: " + fmt.Sprint(s.chosen[p.ID])
}
func (s *Capture) current() workflow.ProviderStatus {
	if s.cursor >= 0 && s.cursor < len(s.statuses) {
		return s.statuses[s.cursor]
	}
	return workflow.ProviderStatus{}
}
func (s *Capture) refresh() tea.Cmd {
	s.busy = true
	return func() tea.Msg {
		report, err := s.session.Status(s.ctx, "")
		return captureStatusMsg{statuses: report.Providers, err: err}
	}
}
func (s *Capture) capture() tea.Cmd {
	ids := make([]string, 0, len(s.chosen))
	for id, chosen := range s.chosen {
		if chosen {
			ids = append(ids, id)
		}
	}
	return func() tea.Msg {
		if s.captureAll {
			_, err := s.session.Capture(s.ctx, "")
			return captureDoneMsg{err: err}
		}
		for _, id := range ids {
			if _, err := s.session.Capture(s.ctx, id); err != nil {
				return captureDoneMsg{err: err}
			}
		}
		return captureDoneMsg{}
	}
}
func (s *Capture) chosenCount() int {
	count := 0
	for _, chosen := range s.chosen {
		if chosen {
			count++
		}
	}
	return count
}
func (s *Capture) confirmCapture() tea.Cmd {
	if s.chosenCount() == 0 {
		return nil
	}
	s.confirm = true
	return func() tea.Msg {
		return components.ModalRequest{Title: "Capture selected providers", Content: components.Confirm("Capture selected running-system changes into this profile?")}
	}
}
