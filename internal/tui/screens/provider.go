package screens

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type providerRow struct{ group, value string }

// Provider presents typed saved state and semantic changes without inventing
// provider-specific policy.
type Provider struct {
	ctx                     context.Context
	session                 *workflow.Session
	id                      string
	width, height, selected int
	tab                     string
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
type CaptureComplete struct {
	Provider string
	Err      error
}

func NewProvider(session *workflow.Session, id string) *Provider {
	return NewProviderContext(context.Background(), session, id)
}
func NewProviderContext(ctx context.Context, session *workflow.Session, id string) *Provider {
	return &Provider{ctx: ctx, session: session, id: id, tab: "Saved"}
}
func (s *Provider) Refresh() tea.Cmd                   { return s.refresh() }
func (s *Provider) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Provider) SetSize(width, height int)          { s.width, s.height = width, height }
func (s *Provider) Init() tea.Cmd                      { return s.refresh() }
func (s *Provider) Update(msg tea.Msg) tea.Cmd {
	if s.tab == "" {
		s.tab = "Saved"
	}
	switch msg := msg.(type) {
	case providerStatusMsg:
		s.status, s.err, s.busy = msg.status, msg.err, false
		s.list.SetSelected(s.selected, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
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
	case "tab":
		if s.tab == "Saved" {
			s.tab = "Changes"
		} else {
			s.tab = "Saved"
		}
		s.list.SetSelected(0, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
	case "j", "down":
		s.list.Move(1, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
	case "k", "up":
		s.list.Move(-1, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
	case "c":
		if !s.busy {
			s.confirm = true
			return func() tea.Msg {
				return components.ModalRequest{Title: "Capture " + titleFor(s.id), Content: components.Confirm("Capture the currently observed " + titleFor(s.id) + " changes into the profile?")}
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
	if s.tab == "" {
		s.tab = "Saved"
	}
	title := titleFor(s.id)
	if s.err != nil {
		return s.styles.Error(title + "\n\n! Unable to load " + title + ": " + s.err.Error())
	}
	lines := []string{title, "[Changes" + countLabel(len(s.status.Changes)) + "] [Saved" + selectedTab(s.tab == "Saved") + "]"}
	if s.busy {
		lines = append(lines, "Loading...")
	}
	if !s.status.Captured {
		lines = append(lines, "No saved state yet.")
	}
	rows := s.rows()
	if len(rows) == 0 {
		rows = []providerRow{{value: emptyTabMessage(s.tab, s.status.Captured)}}
	}
	rendered := make([]string, len(rows))
	for i, row := range rows {
		cursor := " "
		if i == s.list.Selected {
			cursor = ">"
		}
		if row.group != "" {
			rendered[i] = cursor + " " + row.group
		} else {
			rendered[i] = cursor + " " + row.value
		}
	}
	width := s.width
	if width == 0 {
		width = 120
	}
	lines = append(lines, s.list.View(rendered, width, s.listHeight()))
	return strings.Join(lines, "\n")
}
func countLabel(count int) string { return fmt.Sprintf(" %d", count) }
func selectedTab(selected bool) string {
	if selected {
		return " *"
	}
	return ""
}
func emptyTabMessage(tab string, captured bool) string {
	if !captured && tab == "Saved" {
		return "Capture this provider to save its desired state."
	}
	if tab == "Changes" {
		return "✓ No differences detected."
	}
	return "No saved items."
}
func (s *Provider) rows() []providerRow {
	if s.activeTab() == "Changes" {
		rows := make([]providerRow, len(s.status.Changes))
		for i, change := range s.status.Changes {
			rows[i].value = change.Summary
		}
		return rows
	}
	return savedRows(s.status.Snapshot)
}
func (s *Provider) activeTab() string {
	if s.tab == "Changes" {
		return "Changes"
	}
	return "Saved"
}
func savedRows(snapshot any) []providerRow {
	group := func(name string, values []string, rows *[]providerRow) {
		if len(values) == 0 {
			return
		}
		*rows = append(*rows, providerRow{group: name})
		for _, value := range values {
			*rows = append(*rows, providerRow{value: value})
		}
	}
	rows := []providerRow{}
	switch value := snapshot.(type) {
	case profile.Packages:
		group("Official packages", value.Official, &rows)
		group("AUR packages", value.AUR, &rows)
		tools := make([]string, 0, len(value.Mise))
		for tool := range value.Mise {
			tools = append(tools, tool)
		}
		sort.Strings(tools)
		group("Mise tools", tools, &rows)
		group("Machine-specific packages", value.MachineSpecific, &rows)
		group("Excluded packages", value.Excluded, &rows)
	case profile.Themes:
		group("Current theme", []string{empty(value.Current)}, &rows)
		if value.Source != "" {
			group("Source", []string{value.Source}, &rows)
		}
		for _, item := range value.Items {
			group("Saved themes", []string{item.ID + valueSuffix(item.Type, item.Revision)}, &rows)
		}
	case profile.Plugins:
		for _, item := range value.Items {
			group("Saved plugins", []string{item.ID + valueSuffix(item.Source, item.Revision)}, &rows)
		}
	case profile.Defaults:
		group("Applications", []string{"Terminal: " + empty(value.Terminal), "Browser: " + empty(value.Browser), "Editor: " + empty(value.Editor), "Agent: " + empty(value.Agent)}, &rows)
	case profile.Shell:
		group("Saved shell state", []string{fmt.Sprintf("Format version: %d", value.Version), "Snapshot: " + empty(value.Hash), "Baseline: " + empty(value.BaselineHash)}, &rows)
	case profile.Hooks:
		for _, item := range value.Items {
			group("Saved hooks", []string{item.Path}, &rows)
		}
	}
	return rows
}
func valueSuffix(values ...string) string {
	for _, value := range values {
		if value != "" {
			return " (" + value + ")"
		}
	}
	return ""
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
func (s *Provider) capture() tea.Cmd {
	return func() tea.Msg { _, err := s.session.Capture(s.ctx, s.id); return providerCaptureMsg{err} }
}
func (s *Provider) DetailView() string {
	title := titleFor(s.id)
	if s.err != nil {
		return title + " details unavailable: " + s.err.Error()
	}
	if s.tab == "Changes" {
		if change := s.selectedChange(); change.Summary != "" {
			return title + " change\n" + change.Summary
		}
	}
	if row := s.selectedSavedRow(); row.value != "" {
		return title + " saved state\n" + row.value
	}
	return title + " saved state\n" + emptyTabMessage("Saved", s.status.Captured)
}
func (s *Provider) selectedChange() model.Change {
	if s.tab != "Changes" {
		return model.Change{}
	}
	if s.list.Selected >= 0 && s.list.Selected < len(s.status.Changes) {
		return s.status.Changes[s.list.Selected]
	}
	return model.Change{}
}
func (s *Provider) selectedSavedRow() providerRow {
	rows := s.rows()
	if s.tab == "Saved" && s.list.Selected >= 0 && s.list.Selected < len(rows) {
		return rows[s.list.Selected]
	}
	return providerRow{}
}
func (s *Provider) listHeight() int {
	if s.height == 0 {
		return max(1, len(s.rows()))
	}
	return max(1, s.height-2)
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
