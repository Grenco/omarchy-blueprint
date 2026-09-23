package screens

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	width, height                        int
	chosen                               map[string]bool
	table                                components.Table
	navigation                           components.Selectable
	styles                               components.Styles
	confirm, busy, capturing, captureAll bool
	err                                  error
	requestID                            uint64
}

type captureStatusMsg struct {
	requestID uint64
	statuses  []workflow.ProviderStatus
	err       error
}
type captureDoneMsg struct {
	requestID uint64
	providers []string
	err       error
	warning   error
}

func NewCaptureContext(ctx context.Context, session *workflow.Session) *Capture {
	return &Capture{ctx: ctx, session: session, chosen: map[string]bool{}}
}
func (s *Capture) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Capture) SetSize(width, height int) {
	s.width, s.height = width, height
	s.ensureCursorVisible()
}
func (s *Capture) Init() tea.Cmd         { return s.refresh() }
func (s *Capture) TransientActive() bool { return s.confirm || s.busy }
func (s *Capture) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case captureStatusMsg:
		if msg.requestID != s.requestID {
			return nil
		}
		s.statuses, s.err, s.busy = msg.statuses, msg.err, false
		s.cursor = min(s.cursor, max(0, len(s.statuses)-1))
		s.ensureCursorVisible()
		return nil
	case captureDoneMsg:
		if msg.requestID != s.requestID {
			return nil
		}
		s.err, s.busy, s.confirm, s.capturing, s.captureAll = msg.err, false, false, false, false
		if msg.err == nil {
			s.chosen = map[string]bool{}
		}
		return func() tea.Msg { return CaptureComplete{Providers: msg.providers, Err: msg.err, Warning: msg.warning} }
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
		s.ensureCursorVisible()
		return nil
	}
	switch key.String() {
	case "j", "down":
		s.cursor = min(len(s.statuses)-1, s.cursor+1)
		s.ensureCursorVisible()
	case "k", "up":
		s.cursor = max(0, s.cursor-1)
		s.ensureCursorVisible()
	case "space", " ":
		if p := s.current(); p.ID != "" {
			s.chosen[p.ID] = !s.chosen[p.ID]
		}
	case "a":
		s.chosen = map[string]bool{}
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
		return s.styles.Error("Capture failed: " + components.DisplayText(s.err.Error()))
	}
	if s.capturing {
		return "Capturing selected categories...\n\nThis can take a while as categories inspect the running system."
	}
	if s.busy {
		return "Loading capture status..."
	}
	lines := []string{}
	if hint, ok := s.hint(); ok {
		lines = append(lines, hint, "")
	}
	rows := make([]components.Row, 0, len(s.statuses))
	for i, p := range s.statuses {
		selected := i == s.cursor
		changes, style := "not captured", s.styles.Muted
		if p.Captured {
			changes, style = "clean", s.styles.Success
		}
		if p.Captured && len(p.Changes) > 0 {
			changes, style = fmt.Sprintf("%d changed", len(p.Changes)), s.styles.Warning
		}
		if !selected {
			changes = style(changes)
		}
		check := " "
		if s.chosen[p.ID] {
			check = "x"
		}
		rows = append(rows, components.Row{Cells: []string{"[" + check + "]", components.DisplayText(p.ID), changes}, Selected: selected, Focused: true})
	}
	width := s.width
	if width <= 0 {
		width = 60
	}
	table := s.table.Render([]components.Column{{Title: "", Width: 3, MinWidth: 3}, {Title: "CATEGORY", Width: 24, MinWidth: 12}, {Title: "CHANGES", MinWidth: 8}}, rows, width, s.tableRowHeight(), s.styles)
	lines = append(lines, table)
	return strings.Join(lines, "\n")
}

// freshProfile reports whether no category has been captured yet, the
// condition under which the onboarding hint is offered.
func (s *Capture) freshProfile() bool {
	if len(s.statuses) == 0 {
		return false
	}
	for _, status := range s.statuses {
		if status.Captured {
			return false
		}
	}
	return true
}

func (s *Capture) hintWidth() int {
	hintWidth := s.width
	if hintWidth <= 0 || hintWidth > 60 {
		hintWidth = 60
	}
	return hintWidth
}

// hint returns the onboarding hint text and whether it should be shown. At
// constrained heights it compacts to a heading-only line rather than take
// room the category table needs; it is never shown at the expense of a
// category becoming unreachable.
func (s *Capture) hint() (string, bool) {
	if !s.freshProfile() {
		return "", false
	}
	full := renderEmptyState(s.styles, s.hintWidth(), emptyStateCopy{
		Heading:     "Choose what this profile should remember",
		Explanation: "Nothing has been captured yet. Select the categories you want in this profile; you do not need to capture everything.",
	})
	if s.height <= 0 || s.height-hintBlockHeight(full) >= minTableRowHeight {
		return full, true
	}
	compact := renderEmptyState(s.styles, s.hintWidth(), emptyStateCopy{
		Heading: "Choose what this profile should remember",
	})
	return compact, true
}

// minTableRowHeight is the smallest table height (header plus data rows)
// Capture tries to preserve for the category table before compacting or
// dropping onboarding prose; the table itself is never hidden below this.
const minTableRowHeight = 4

func hintBlockHeight(hint string) int {
	// +1 for the blank separator line View() appends after the hint.
	return strings.Count(hint, "\n") + 1 + 1
}

// tableRowHeight is the header+data-row height passed to Table.Render,
// budgeted from the remaining space after any onboarding hint.
func (s *Capture) tableRowHeight() int {
	if s.height <= 0 {
		return max(2, len(s.statuses)+1)
	}
	budget := s.height
	if hint, ok := s.hint(); ok {
		budget -= hintBlockHeight(hint)
	}
	return max(2, budget)
}

// ensureCursorVisible keeps the selected category within the table's
// scrolled viewport using the table's own Offset, rather than a second
// scrolling mechanism.
func (s *Capture) ensureCursorVisible() {
	s.table.Ensure(s.cursor, len(s.statuses), max(1, s.tableRowHeight()-1))
}
func (s *Capture) DetailView() string {
	p := s.current()
	if p.ID == "" {
		return "Select categories to capture."
	}
	selected := "No"
	if s.chosen[p.ID] {
		selected = "Yes"
	}
	return "Category: " + components.DisplayText(p.ID) + "\nChanges: " + fmt.Sprint(len(p.Changes)) + "\nSelected for capture: " + selected
}
func (s *Capture) current() workflow.ProviderStatus {
	if s.cursor >= 0 && s.cursor < len(s.statuses) {
		return s.statuses[s.cursor]
	}
	return workflow.ProviderStatus{}
}
func (s *Capture) refresh() tea.Cmd {
	s.requestID++
	requestID := s.requestID
	s.busy = true
	return func() tea.Msg {
		report, err := s.session.CaptureStatus(s.ctx)
		return captureStatusMsg{requestID: requestID, statuses: report.Providers, err: err}
	}
}
func (s *Capture) capture() tea.Cmd {
	s.requestID++
	requestID := s.requestID
	ids := make([]string, 0, len(s.statuses))
	for _, status := range s.statuses {
		if s.captureAll || s.chosen[status.ID] {
			ids = append(ids, status.ID)
		}
	}
	return func() tea.Msg {
		result, err := s.session.CaptureMany(s.ctx, ids)
		var warning workflow.PostCommitWarning
		if errors.As(err, &warning) {
			return captureDoneMsg{requestID: requestID, providers: result.Providers, warning: warning.Err}
		}
		return captureDoneMsg{requestID: requestID, providers: result.Providers, err: err}
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
		return components.ModalRequest{Title: "Capture selected categories", Content: components.Confirm("Capture selected running-system changes into this profile?")}
	}
}
