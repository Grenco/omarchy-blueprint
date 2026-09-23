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
	count   int
	item    workflow.AttentionItem
	healthy string
}

// Overview presentation groups, in display order.
const (
	overviewNeedsAttention   = "Needs attention"
	overviewChangesAvailable = "Changes available"
	overviewIntentional      = "Intentional differences"
	overviewNoAction         = "No action needed"
)

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
	requestID uint64
}
type overviewMsg struct {
	requestID uint64
	data      workflow.Overview
	err       error
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
		if result.requestID != s.requestID {
			return nil
		}
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
	if key.String() == "[" || key.String() == "]" {
		if s.list.JumpToAnchor(s.groupAnchors(), key.String() == "]", len(s.rows()), s.listHeight()) {
			s.selected = s.list.Selected
		}
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
			s.collapsed[row.section] = !s.isCollapsed(row.section)
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
		return "Unable to load overview: " + components.DisplayText(s.err.Error())
	}
	if s.session != nil && !profileHasCapturedState(s.session.Profile()) {
		return renderEmptyState(s.styles, s.width, emptyStateCopy{
			Heading:     "Nothing has been captured yet",
			Explanation: "Capture is where you choose which parts of this machine Blueprint should remember. You can still explore the other sections to understand what each category covers.",
			Guidance:    "Next: open Capture when you're ready to save something.",
		})
	}
	width := s.width
	if width == 0 {
		width = 120
	}
	lines := make([]string, 0, len(s.rows()))
	for i, row := range s.rows() {
		selected := i == s.list.Selected
		switch {
		case row.isSection():
			marker := components.Icons.Expanded
			if s.isCollapsed(row.section) {
				marker = components.Icons.Collapsed
			}
			line := fmt.Sprintf("%s %s (%d)", marker, row.section, row.count)
			if !selected {
				line = s.styles.Accent(line)
			}
			line = components.PadLine(line, width)
			if selected {
				if !s.styles.Palette.ColorEnabled {
					line = components.Icons.Selected + line
				}
				line = s.styles.Selection(line, true)
			}
			lines = append(lines, line)
		case row.isItem():
			glyph, summary := attentionGlyph(row.item.Severity), decisionSummary(row.item)
			if !selected {
				glyph = attentionStyle(s.styles, row.item.Severity)(glyph)
				if row.item.Severity == workflow.AttentionIntentional {
					// Intentional differences are informative, never a warning.
					summary = s.styles.Muted(summary)
				}
			}
			line := components.PadLine("  "+glyph+" "+summary, width)
			if selected {
				if !s.styles.Palette.ColorEnabled {
					line = components.Icons.Selected + line[1:]
				}
				line = s.styles.Selection(line, true)
			}
			lines = append(lines, line)
		default:
			glyph := components.Icons.Ready
			if !selected {
				glyph = s.styles.Success(glyph)
			}
			line := components.PadLine("  "+glyph+" "+row.healthy, width)
			if selected {
				line = s.styles.Selection(line, true)
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "✓ Nothing needs review.")
	}
	return s.list.View(lines, width, s.listHeight())
}
func (s *Overview) DetailView() string {
	row := s.selectedRow()
	if row.isSection() {
		lines := []string{components.DisplayText(row.section), overviewSectionMeaning[row.section], "", "Press Enter to " + map[bool]string{true: "expand", false: "collapse"}[s.isCollapsed(row.section)] + " this section."}
		return strings.Join(lines, "\n")
	}
	if !row.isItem() {
		if row.healthy != "" {
			return "Healthy\n" + components.DisplayText(row.healthy) + " has no detected differences."
		}
		return "Overview details\nSelect a decision to see what needs attention."
	}
	heading := "Needs attention"
	switch row.item.Severity {
	case workflow.AttentionDrift, workflow.AttentionInfo:
		heading = "Change available"
	case workflow.AttentionIntentional:
		heading = "Intentional difference"
	}
	lines := []string{heading, decisionSummary(row.item), "", "Why: " + components.DisplayText(row.item.Summary)}
	if row.item.Reason != "" {
		lines = append(lines, "Policy: "+components.DisplayText(row.item.Reason), "", "This machine is meant to differ here, so it is not a warning.")
	}
	if row.item.Target != "" {
		lines = append(lines, "", "Enter opens: "+components.DisplayText(row.item.Target))
	}
	return strings.Join(lines, "\n")
}

// attentionGlyph and attentionStyle give each attention item a scannable
// icon and colour by urgency: Decision/Warning need a choice, Drift/Info are
// informational only.
func attentionGlyph(severity workflow.AttentionSeverity) string {
	if severity == workflow.AttentionDecision || severity == workflow.AttentionWarning {
		return components.Icons.Attention
	}
	return components.Icons.Changed
}
func attentionStyle(styles components.Styles, severity workflow.AttentionSeverity) func(string) string {
	switch severity {
	case workflow.AttentionDecision, workflow.AttentionWarning:
		return styles.Warning
	case workflow.AttentionIntentional:
		return styles.Muted
	}
	return styles.Accent
}
func decisionSummary(item workflow.AttentionItem) string {
	if item.Provider == "" {
		return components.DisplayText(item.Summary)
	}
	return components.DisplayText(title(item.Provider)) + ": " + components.DisplayText(item.Summary)
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
func (s *Overview) SelectedIsSection() bool              { return s.selectedRow().isSection() }
func (s *Overview) groupAnchors() []int {
	anchors := []int{}
	for i, row := range s.rows() {
		if row.isSection() {
			anchors = append(anchors, i)
		}
	}
	return anchors
}
func (s *Overview) rows() []overviewRow {
	sections := []struct {
		title string
		show  func(workflow.AttentionItem) bool
	}{
		{overviewNeedsAttention, func(item workflow.AttentionItem) bool {
			return item.Severity == workflow.AttentionDecision || item.Severity == workflow.AttentionWarning
		}},
		{overviewChangesAvailable, func(item workflow.AttentionItem) bool {
			return item.Severity == workflow.AttentionDrift || item.Severity == workflow.AttentionInfo
		}},
		{overviewIntentional, func(item workflow.AttentionItem) bool { return item.Severity == workflow.AttentionIntentional }},
		{overviewNoAction, nil},
	}
	rows := make([]overviewRow, 0, len(s.data.Items)+len(s.data.Healthy)+4)
	for _, section := range sections {
		members := []overviewRow{}
		if section.show == nil {
			for _, healthy := range s.data.Healthy {
				members = append(members, overviewRow{healthy: healthy})
			}
		} else {
			for _, item := range s.data.Items {
				if section.show(item) {
					members = append(members, overviewRow{item: item})
				}
			}
		}
		if len(members) == 0 && section.title == overviewIntentional {
			continue
		}
		rows = append(rows, overviewRow{section: section.title, count: len(members)})
		if !s.isCollapsed(section.title) {
			rows = append(rows, members...)
		}
	}
	return rows
}

// isCollapsed defaults Intentional differences to collapsed: they are
// deliberate and need no action, so they stay out of the way until asked for.
func (s *Overview) isCollapsed(section string) bool {
	if collapsed, ok := s.collapsed[section]; ok {
		return collapsed
	}
	return section == overviewIntentional
}

var overviewSectionMeaning = map[string]string{
	overviewNeedsAttention:   "Blocked, unsafe, or unresolved items that need a decision.",
	overviewChangesAvailable: "Differences that Capture or Restore would act on under current policy.",
	overviewIntentional:      "Differences that policy deliberately leaves alone on this machine.",
	overviewNoAction:         "Categories with nothing to act on.",
}

func (s *Overview) HeaderState() string {
	if s.busy {
		return "~ overview loading"
	}
	if s.err != nil {
		return "x overview error"
	}
	if s.session != nil && !profileHasCapturedState(s.session.Profile()) {
		return "~ nothing captured"
	}
	attention, changes := 0, 0
	for _, item := range s.data.Items {
		switch item.Severity {
		case workflow.AttentionDecision, workflow.AttentionWarning:
			attention++
		case workflow.AttentionDrift, workflow.AttentionInfo:
			changes++
		}
	}
	switch {
	case attention > 0:
		return fmt.Sprintf("! %d need attention", attention)
	case changes > 0:
		return fmt.Sprintf("~ %d changes available", changes)
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
	s.requestID++
	requestID := s.requestID
	s.busy = true
	return func() tea.Msg {
		data, err := s.session.Overview(s.ctx)
		return overviewMsg{requestID: requestID, data: data, err: err}
	}
}
