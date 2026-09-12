package screens

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// OverviewTarget is handled by the root model, keeping this screen independent
// from the application's ScreenID type.
type OverviewTarget struct{ Target, Ref string }

type Overview struct {
	ctx      context.Context
	session  *workflow.Session
	data     workflow.Overview
	selected int
	err      error
	busy     bool
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
func (s *Overview) SetSize(int, int) {}
func (s *Overview) Init() tea.Cmd    { return s.refresh() }
func (s *Overview) Update(msg tea.Msg) tea.Cmd {
	if result, ok := msg.(overviewMsg); ok {
		s.data, s.err, s.busy = result.data, result.err, false
		if s.selected >= len(s.orderedItems()) {
			s.selected = max(0, len(s.orderedItems())-1)
		}
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		if s.selected < len(s.orderedItems())-1 {
			s.selected++
		}
	case "k", "up":
		if s.selected > 0 {
			s.selected--
		}
	case "r":
		return s.refresh()
	case "enter":
		if item := s.selectedItem(); item.Target != "" {
			return func() tea.Msg { return OverviewTarget{Target: item.Target, Ref: item.Ref} }
		}
	}
	return nil
}
func (s *Overview) View() string {
	if s.err != nil {
		return "Overview\n\nUnable to load Overview: " + s.err.Error()
	}
	lines := []string{"Overview", ""}
	for _, group := range []struct {
		title string
		show  func(workflow.AttentionItem) bool
	}{{"Needs review", func(item workflow.AttentionItem) bool {
		return item.Severity == workflow.AttentionDecision || item.Severity == workflow.AttentionWarning || (item.Severity == workflow.AttentionInfo && item.Provider != "profile-git")
	}}, {"Drift", func(item workflow.AttentionItem) bool { return item.Severity == workflow.AttentionDrift }}, {"Profile sync", func(item workflow.AttentionItem) bool { return item.Provider == "profile-git" }}, {"Healthy", nil}} {
		lines = append(lines, group.title)
		if group.show == nil {
			for _, healthy := range s.data.Healthy {
				lines = append(lines, "  "+healthy)
			}
			continue
		}
		for i, item := range s.orderedItems() {
			if group.show(item) {
				marker := " "
				if i == s.selected {
					marker = ">"
				}
				lines = append(lines, marker+" "+item.Summary)
			}
		}
	}
	lines = append(lines, "", "enter open  r refresh  j/k select")
	return strings.Join(lines, "\n")
}
func (s *Overview) DetailView() string {
	item := s.selectedItem()
	if item.Summary == "" {
		return "Overview details\nSelect an item to see its destination and reason."
	}
	return strings.Join([]string{"Attention item", "Severity: " + string(item.Severity), "Provider: " + item.Provider, "Kind: " + item.Kind, "Reference: " + item.Ref, "", item.Summary, "", "Enter opens: " + item.Target}, "\n")
}
func (s *Overview) selectedItem() workflow.AttentionItem {
	items := s.orderedItems()
	if s.selected >= 0 && s.selected < len(items) {
		return items[s.selected]
	}
	return workflow.AttentionItem{}
}
func (s *Overview) orderedItems() []workflow.AttentionItem {
	items := make([]workflow.AttentionItem, 0, len(s.data.Items))
	for _, match := range []func(workflow.AttentionItem) bool{
		func(item workflow.AttentionItem) bool {
			return item.Severity == workflow.AttentionDecision || item.Severity == workflow.AttentionWarning || (item.Severity == workflow.AttentionInfo && item.Provider != "profile-git")
		},
		func(item workflow.AttentionItem) bool { return item.Severity == workflow.AttentionDrift },
		func(item workflow.AttentionItem) bool { return item.Provider == "profile-git" },
	} {
		for _, item := range s.data.Items {
			if match(item) {
				items = append(items, item)
			}
		}
	}
	return items
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
func (s *Overview) refresh() tea.Cmd {
	s.busy = true
	return func() tea.Msg { data, err := s.session.Overview(s.ctx); return overviewMsg{data, err} }
}
