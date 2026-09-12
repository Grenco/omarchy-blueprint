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

type providerRow struct{ group, section, value, key, state string }

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
	collapsed               map[string]bool
	err                     error
}
type providerStatusMsg struct {
	status workflow.ProviderStatus
	err    error
}
type providerCaptureMsg struct{ err error }
type providerToggleMsg struct{ err error }
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
	case providerToggleMsg:
		s.err, s.busy = msg.err, false
		if msg.err == nil {
			return s.refresh()
		}
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
	case "enter":
		if s.confirm {
			s.confirm, s.busy = false, true
			return s.capture()
		}
		if row := s.selectedSavedRow(); row.group != "" {
			if s.collapsed == nil {
				s.collapsed = map[string]bool{}
			}
			s.collapsed[row.group] = !s.collapsed[row.group]
			s.list.SetSelected(s.selected, len(s.rows()), s.listHeight())
			s.selected = s.list.Selected
		}
	case "space", " ":
		if row := s.selectedSavedRow(); s.activeTab() == "Saved" && row.value != "" && providerItemCanToggle(s.id, row.section) && !s.busy {
			s.busy = true
			return s.toggleItem(row)
		}
		if s.activeTab() == "Saved" && providerCanToggle(s.id) && !s.busy {
			s.busy = true
			return s.toggleCaptured()
		}
	case "c":
		if !s.busy {
			s.confirm = true
			return func() tea.Msg {
				return components.ModalRequest{Title: "Capture " + titleFor(s.id), Content: components.Confirm("Capture the currently observed " + titleFor(s.id) + " changes into the profile?")}
			}
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
	if s.err != nil {
		return s.styles.Error("! Unable to load " + titleFor(s.id) + ": " + s.err.Error())
	}
	lines := []string{components.TabBar([]string{"Changes" + countLabel(len(s.status.Changes)), "Saved"}, s.tabLabel(), s.styles)}
	if s.tab == "Saved" && providerItemCanToggleID(s.id) {
		lines = append(lines, "+ included in Blueprint   - not included in Blueprint")
	}
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
		if row.group != "" {
			icon := components.Icons.Expanded
			if s.collapsed[row.group] {
				icon = components.Icons.Collapsed
			}
			rendered[i] = s.styles.Accent(icon + " " + row.group)
			if i == s.list.Selected {
				if !s.styles.Palette.ColorEnabled {
					rendered[i] = components.Icons.Selected + rendered[i]
				}
				rendered[i] = s.styles.Selection(rendered[i], true)
			}
		} else {
			marker := "  "
			if row.state == "included" {
				marker = s.styles.Added("+ ")
			}
			if row.state == "not included" {
				marker = s.styles.Removed("- ")
			}
			rendered[i] = marker + row.value
			if i == s.list.Selected {
				if !s.styles.Palette.ColorEnabled {
					rendered[i] = components.Icons.Selected + rendered[i][1:]
				}
				rendered[i] = s.styles.Selection(rendered[i], true)
			}
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
func (s *Provider) tabLabel() string {
	if s.tab == "Changes" {
		return "Changes" + countLabel(len(s.status.Changes))
	}
	return "Saved"
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
	rows := savedRows(s.status.Snapshot)
	if len(s.collapsed) == 0 {
		return rows
	}
	visible := make([]providerRow, 0, len(rows))
	collapsed := false
	for _, row := range rows {
		if row.group != "" {
			collapsed = s.collapsed[row.group]
			visible = append(visible, row)
			continue
		}
		if !collapsed {
			visible = append(visible, row)
		}
	}
	return visible
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
			*rows = append(*rows, providerRow{section: name, value: value, key: value})
		}
	}
	rows := []providerRow{}
	switch value := snapshot.(type) {
	case profile.Packages:
		packageRows := func(section, kind string, values []string) {
			items, states := append([]string{}, values...), map[string]string{}
			for _, ref := range value.Excluded {
				if strings.HasPrefix(ref, kind+":") {
					name := strings.TrimPrefix(ref, kind+":")
					present := false
					for _, item := range items {
						if item == name {
							present = true
							break
						}
					}
					if !present {
						items = append(items, name)
					}
					states[name] = "not included"
				}
			}
			sort.SliceStable(items, func(i, j int) bool { return states[items[i]] != "not included" && states[items[j]] == "not included" })
			group(section, items, &rows)
			for i := range rows {
				if rows[i].section == section {
					rows[i].state = "included"
					if states[rows[i].key] != "" {
						rows[i].state = states[rows[i].key]
					}
				}
			}
		}
		packageRows("Official packages", "official", value.Official)
		packageRows("AUR packages", "aur", value.AUR)
		tools := make([]string, 0, len(value.Mise))
		for tool := range value.Mise {
			tools = append(tools, tool)
		}
		sort.Strings(tools)
		packageRows("Mise tools", "mise", tools)
		group("Machine-specific packages", value.MachineSpecific, &rows)
		for i := range rows {
			if rows[i].section == "Machine-specific packages" {
				rows[i].state = "included"
			}
		}
	case profile.Themes:
		group("Current theme", []string{empty(value.Current)}, &rows)
		themes := make([]string, 0, len(value.Items))
		for _, item := range value.Items {
			if !containsValue(value.Excluded, item.ID) {
				themes = append(themes, item.ID+valueSuffix(item.Type, item.Revision))
			}
		}
		for _, item := range value.Items {
			if containsValue(value.Excluded, item.ID) {
				themes = append(themes, item.ID+valueSuffix(item.Type, item.Revision))
			}
		}
		group("Saved themes", themes, &rows)
		for i := range rows {
			if rows[i].group == "" {
				rows[i].key = strings.Split(rows[i].value, " (")[0]
				if rows[i].section == "Saved themes" {
					for _, item := range value.Items {
						if item.ID == rows[i].key {
							rows[i].state = map[bool]string{true: "not included", false: "included"}[containsValue(value.Excluded, item.ID)]
						}
					}
				}
			}
		}
	case profile.Plugins:
		pluginRows := func(label, source string) {
			plugins := make([]string, 0)
			for _, item := range value.Items {
				if item.Source == source && !containsValue(value.Excluded, item.ID) {
					plugins = append(plugins, item.ID+valueSuffix(item.Revision))
				}
			}
			for _, item := range value.Items {
				if item.Source == source && containsValue(value.Excluded, item.ID) {
					plugins = append(plugins, item.ID+valueSuffix(item.Revision))
				}
			}
			group(label, plugins, &rows)
		}
		pluginRows("Git plugins", "git")
		pluginRows("Local plugins", "local")
		pluginRows("Built-in plugins", "builtin")
		for i := range rows {
			if rows[i].group == "" {
				rows[i].key = strings.Split(rows[i].value, " (")[0]
				if strings.HasSuffix(rows[i].section, " plugins") {
					for _, item := range value.Items {
						if item.ID == rows[i].key {
							rows[i].state = map[bool]string{true: "not included", false: "included"}[containsValue(value.Excluded, item.ID)]
						}
					}
				}
			}
		}
	case profile.Defaults:
		group("Applications", []string{"Terminal: " + empty(value.Terminal), "Browser: " + empty(value.Browser), "Editor: " + empty(value.Editor), "Agent: " + empty(value.Agent)}, &rows)
	case profile.Shell:
		group("Saved shell state", []string{fmt.Sprintf("Format version: %d", value.Version), "Snapshot: " + empty(value.Hash), "Baseline: " + empty(value.BaselineHash)}, &rows)
	case profile.Hooks:
		hooks := make([]string, 0, len(value.Items))
		for _, item := range value.Items {
			hooks = append(hooks, item.Path)
		}
		group("Saved hooks", hooks, &rows)
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
func (s *Provider) toggleCaptured() tea.Cmd {
	captured := !providerCaptured(s.session.Profile(), s.id)
	return func() tea.Msg { return providerToggleMsg{err: s.session.SetProviderCaptured(s.ctx, s.id, captured)} }
}
func (s *Provider) toggleItem(row providerRow) tea.Cmd {
	return func() tea.Msg {
		return providerToggleMsg{err: s.session.SetProviderItemEnabled(s.ctx, s.id, row.section, row.key)}
	}
}
func providerCanToggle(id string) bool {
	switch id {
	case "themes", "plugins", "shell", "hooks", "defaults":
		return true
	}
	return false
}
func providerItemCanToggle(id, section string) bool {
	if id == "packages" {
		return section == "Official packages" || section == "AUR packages" || section == "Mise tools" || section == "Machine-specific packages" || section == "Excluded packages"
	}
	if id == "themes" {
		return section == "Saved themes"
	}
	return id == "plugins" && strings.HasSuffix(section, " plugins")
}
func providerItemCanToggleID(id string) bool {
	return id == "packages" || id == "themes" || id == "plugins"
}
func containsValue(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
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
