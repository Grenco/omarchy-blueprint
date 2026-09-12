package screens

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// OverviewTarget is handled by the root model, keeping this screen independent
// from the application's ScreenID type.
type OverviewTarget struct{ Target, Ref string }

type overviewRow struct {
	section string
	item    workflow.AttentionItem
	healthy string
}

func (r overviewRow) isSection() bool { return r.section != "" }
func (r overviewRow) isItem() bool    { return r.item.Summary != "" }

type Overview struct {
	ctx       context.Context
	session   *workflow.Session
	data      workflow.Overview
	width     int
	height    int
	selected  int
	collapsed map[string]bool
	list      components.Selectable
	styles    components.Styles
	err       error
	busy      bool
}
type overviewMsg struct {
	data workflow.Overview
	err  error
}

func NewOverview(session *workflow.Session) *Overview {
	return NewOverviewContext(context.Background(), session)
}
func NewOverviewContext(ctx context.Context, session *workflow.Session) *Overview {
	return &Overview{ctx: ctx, session: session}
}
func (s *Overview) SetSize(width, height int)          { s.width, s.height = width, height }
func (s *Overview) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Overview) Init() tea.Cmd                      { return s.refresh() }
func (s *Overview) Update(msg tea.Msg) tea.Cmd {
	if result, ok := msg.(overviewMsg); ok {
		s.data, s.err, s.busy = result.data, result.err, false
		s.list.SetSelected(s.selected, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if s.list.Vim(key.String(), len(s.rows()), s.listHeight()) {
		s.selected = s.list.Selected
		return nil
	}
	switch key.String() {
	case "j", "down":
		s.list.Move(1, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
	case "k", "up":
		s.list.Move(-1, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
	case "r":
		return s.refresh()
	case "enter":
		row := s.selectedRow()
		if row.isSection() {
			if s.collapsed == nil {
				s.collapsed = map[string]bool{}
			}
			s.collapsed[row.section] = !s.collapsed[row.section]
			s.list.SetSelected(s.selected, len(s.rows()), s.listHeight())
			s.selected = s.list.Selected
			return nil
		}
		if row.isItem() && row.item.Target != "" {
			return func() tea.Msg { return OverviewTarget{Target: row.item.Target, Ref: row.item.Ref} }
		}
	}
	return nil
}
func (s *Overview) View() string {
	if s.err != nil {
		return "Unable to load overview: " + s.err.Error()
	}
	lines := make([]string, 0, len(s.rows()))
	for i, row := range s.rows() {
		switch {
		case row.isSection():
			marker := components.Icons.Expanded
			if s.collapsed[row.section] {
				marker = components.Icons.Collapsed
			}
			line := s.styles.Accent(marker + " " + row.section)
			if i == s.list.Selected {
				if !s.styles.Palette.ColorEnabled {
					line = components.Icons.Selected + line
				}
				line = s.styles.Selection(line, true)
			}
			lines = append(lines, line)
		case row.isItem():
			line := "  " + decisionSummary(row.item)
			if i == s.list.Selected {
				if !s.styles.Palette.ColorEnabled {
					line = components.Icons.Selected + line[1:]
				}
				line = s.styles.Selection(line, true)
			}
			lines = append(lines, line)
		default:
			line := "  " + components.Icons.Ready + " " + row.healthy
			if i == s.list.Selected {
				line = s.styles.Selection(line, true)
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "✓ Nothing needs review.")
	}
	width := s.width
	if width == 0 {
		width = 120
	}
	return s.list.View(lines, width, s.listHeight())
}
func (s *Overview) DetailView() string {
	row := s.selectedRow()
	if row.isSection() {
		return row.section + "\nPress Enter to " + map[bool]string{true: "expand", false: "collapse"}[s.collapsed[row.section]] + " this section."
	}
	if !row.isItem() {
		if row.healthy != "" {
			return "Healthy\n" + row.healthy + " has no detected differences."
		}
		return "Overview details\nSelect a decision to see what needs attention."
	}
	lines := []string{"Needs attention", decisionSummary(row.item), "", "Why: " + row.item.Summary}
	if row.item.Target != "" {
		lines = append(lines, "", "Enter opens: "+row.item.Target)
	}
	return strings.Join(lines, "\n")
}
func decisionSummary(item workflow.AttentionItem) string {
	if item.Provider == "" {
		return item.Summary
	}
	return title(item.Provider) + ": " + item.Summary
}
func title(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + strings.ReplaceAll(value[1:], "-", " ")
}
func (s *Overview) selectedRow() overviewRow {
	rows := s.rows()
	if s.list.Selected >= 0 && s.list.Selected < len(rows) {
		return rows[s.list.Selected]
	}
	return overviewRow{}
}
func (s *Overview) selectedItem() workflow.AttentionItem { return s.selectedRow().item }
func (s *Overview) CanOpen() bool                        { return s.selectedItem().Target != "" }
func (s *Overview) rows() []overviewRow {
	sections := []struct {
		title string
		show  func(workflow.AttentionItem) bool
	}{
		{"Needs review", func(item workflow.AttentionItem) bool {
			return item.Severity == workflow.AttentionDecision || item.Severity == workflow.AttentionWarning || (item.Severity == workflow.AttentionInfo && item.Provider != "profile-git")
		}},
		{"Drift", func(item workflow.AttentionItem) bool { return item.Severity == workflow.AttentionDrift }},
		{"Profile sync", func(item workflow.AttentionItem) bool { return item.Provider == "profile-git" }},
		{"Healthy", nil},
	}
	rows := make([]overviewRow, 0, len(s.data.Items)+len(s.data.Healthy)+4)
	for _, section := range sections {
		rows = append(rows, overviewRow{section: section.title})
		if s.collapsed[section.title] {
			continue
		}
		if section.show == nil {
			for _, healthy := range s.data.Healthy {
				rows = append(rows, overviewRow{healthy: healthy})
			}
			continue
		}
		for _, item := range s.data.Items {
			if section.show(item) {
				rows = append(rows, overviewRow{item: item})
			}
		}
	}
	return rows
}
func (s *Overview) HeaderState() string {
	if s.busy {
		return "~ overview loading"
	}
	if s.err != nil {
		return "x overview error"
	}
	if len(s.data.Items) > 0 {
		return fmt.Sprintf("! %d attention", len(s.data.Items))
	}
	return "✓ overview clean"
}
func (s *Overview) listHeight() int {
	if s.height == 0 {
		return max(1, len(s.rows()))
	}
	return max(1, s.height-1)
}
func (s *Overview) refresh() tea.Cmd {
	s.busy = true
	return func() tea.Msg { data, err := s.session.Overview(s.ctx); return overviewMsg{data, err} }
}
