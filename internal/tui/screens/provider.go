package screens

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type providerRow struct {
	group, section, value, key, state string
	target                            workflow.TargetInspection
	effective                         policy.EffectiveSetting
}

// Provider presents typed saved state and semantic changes without inventing
// provider-specific policy.
type Provider struct {
	ctx                     context.Context
	session                 *workflow.Session
	id                      string
	width, height, selected int
	tab                     string
	status                  workflow.ProviderStatus
	targets                 []workflow.TargetInspection
	effective               map[string]policy.Effective
	list                    components.Selectable
	styles                  components.Styles
	busy, confirm           bool
	collapsed               map[string]bool
	err                     error
	requestID               uint64
}
type providerStatusMsg struct {
	requestID uint64
	status    workflow.ProviderStatus
	targets   []workflow.TargetInspection
	effective map[string]policy.Effective
	err       error
}
type providerCaptureMsg struct {
	requestID uint64
	err       error
	warning   error
}
type providerToggleMsg struct {
	requestID uint64
	err       error
}
type CaptureComplete struct {
	Provider  string
	Providers []string
	Err       error
	Warning   error
}

func NewProvider(session *workflow.Session, id string) *Provider {
	return NewProviderContext(context.Background(), session, id)
}
func NewProviderContext(ctx context.Context, session *workflow.Session, id string) *Provider {
	return &Provider{ctx: ctx, session: session, id: id, tab: "State"}
}
func (s *Provider) Refresh() tea.Cmd                   { return s.refresh() }
func (s *Provider) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Provider) SetSize(width, height int)          { s.width, s.height = width, height }
func (s *Provider) Init() tea.Cmd                      { return s.refresh() }
func (s *Provider) TransientActive() bool              { return s.confirm }
func (s *Provider) Update(msg tea.Msg) tea.Cmd {
	if s.tab == "" {
		s.tab = "State"
	}
	switch msg := msg.(type) {
	case providerStatusMsg:
		if msg.requestID != s.requestID {
			return nil
		}
		s.status, s.targets, s.effective, s.err, s.busy = msg.status, msg.targets, msg.effective, msg.err, false
		s.list.SetSelected(s.selected, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
		return nil
	case providerCaptureMsg:
		if msg.requestID != s.requestID {
			return nil
		}
		s.err, s.busy = msg.err, false
		if msg.err == nil {
			return func() tea.Msg { return CaptureComplete{Provider: s.id, Warning: msg.warning} }
		}
		return nil
	case providerToggleMsg:
		if msg.requestID != s.requestID {
			return nil
		}
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
	if s.confirm {
		switch key.String() {
		case "enter":
			s.confirm, s.busy = false, true
			return s.capture()
		case "esc":
			s.confirm = false
		}
		return nil
	}
	if key.String() == "[" || key.String() == "]" {
		if s.list.JumpToAnchor(s.groupAnchors(), key.String() == "]", len(s.rows()), s.listHeight()) {
			s.selected = s.list.Selected
		}
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
		s.tab = nextProviderTab(s.tab)
		s.list.SetSelected(0, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
	case "j", "down":
		s.list.Move(1, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
	case "k", "up":
		s.list.Move(-1, len(s.rows()), s.listHeight())
		s.selected = s.list.Selected
	case "enter":
		if row := s.selectedSavedRow(); row.group != "" {
			if s.collapsed == nil {
				s.collapsed = map[string]bool{}
			}
			s.collapsed[row.group] = !s.collapsed[row.group]
			s.list.SetSelected(s.selected, len(s.rows()), s.listHeight())
			s.selected = s.list.Selected
		}
	case "space", " ":
		if s.policyTab() && !s.busy {
			if row := s.selectedPolicyRow(); row.key != "" {
				s.busy = true
				return s.setPolicy(row)
			}
			break
		}
		if row := s.selectedSavedRow(); s.activeTab() == "Saved" && row.value != "" && providerItemCanToggle(s.id, row.section) && !s.busy {
			s.busy = true
			return s.toggleItem(row)
		}
	case "x":
		if s.policyTab() && !s.busy {
			if row := s.selectedPolicyRow(); row.key != "" {
				s.busy = true
				return s.clearPolicy(row)
			}
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
		s.tab = "State"
	}
	if s.err != nil {
		return s.styles.Error("! Unable to load " + titleFor(s.id) + ": " + components.DisplayText(s.err.Error()))
	}
	if s.policyTab() {
		return s.policyView()
	}
	lines := []string{components.TabBar([]string{"State", "Capture", "Restore"}, "State", s.styles)}
	if providerItemCanToggleID(s.id) {
		lines = append(lines, "+ included in Blueprint   - not included in Blueprint")
	}
	if s.busy {
		lines = append(lines, "Loading...")
	}
	if !s.busy && len(s.targets) == 0 {
		if copy, ok := providerEmptyState(s.id, s.status, s.activeTab()); ok {
			lines = append(lines, renderEmptyState(s.styles, s.width, copy))
			return strings.Join(lines, "\n")
		}
	}
	rows := s.rows()
	if len(rows) == 0 {
		if message := emptyTabMessage(s.tab, s.status.Captured); message != "" {
			rows = []providerRow{{value: message}}
		}
	}
	rendered := make([]string, len(rows))
	for i, row := range rows {
		if row.group != "" {
			icon := components.Icons.Expanded
			if s.collapsed[row.group] {
				icon = components.Icons.Collapsed
			}
			rendered[i] = s.styles.Accent(icon + " " + components.DisplayText(row.group))
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
			rendered[i] = marker + components.DisplayText(row.value)
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
func (s *Provider) policyView() string {
	lines := []string{
		components.TabBar([]string{"State", "Capture", "Restore"}, s.tab, s.styles),
		s.policyScopeLabel(),
		"space: change policy   x: reset override",
	}
	if s.busy {
		lines = append(lines, "Loading...")
	}
	rows := s.rows()
	if len(rows) == 0 {
		rows = []providerRow{{value: components.RenderPolicyStatus(s.tab, policy.EffectiveSetting{Enabled: true}, "")}}
	}
	rendered := make([]string, len(rows))
	for i, row := range rows {
		if row.group != "" {
			rendered[i] = s.styles.Accent(components.Icons.Expanded + " " + components.DisplayText(row.group))
		} else {
			rendered[i] = components.DisplayText(row.value)
		}
		if i == s.list.Selected {
			if !s.styles.Palette.ColorEnabled {
				rendered[i] = components.Icons.Selected + rendered[i]
			}
			rendered[i] = s.styles.Selection(rendered[i], true)
		}
	}
	width := s.width
	if width == 0 {
		width = 80
	}
	return strings.Join(append(lines, s.list.View(rendered, width, s.listHeight())), "\n")
}
func countLabel(count int) string { return fmt.Sprintf(" %d", count) }
func (s *Provider) tabLabel() string {
	return "State"
}
func nextProviderTab(tab string) string {
	switch tab {
	case "State", "Saved", "Changes", "":
		return "Capture"
	case "Capture":
		return "Restore"
	default:
		return "State"
	}
}
func (s *Provider) policyTab() bool { return s.tab == "Capture" || s.tab == "Restore" }
func (s *Provider) policyScopeLabel() string {
	if s.session != nil && s.session.Machine().Name != "" {
		return "Machine: " + s.session.Machine().Name
	}
	return "Profile defaults"
}
func emptyTabMessage(tab string, captured bool) string {
	if !captured {
		return ""
	}
	if tab == "Changes" {
		return "✓ No differences detected."
	}
	return "No saved items."
}
func (s *Provider) rows() []providerRow {
	if s.policyTab() {
		return s.policyRows()
	}
	if s.activeTab() == "Saved" && len(s.targets) > 0 {
		return s.targetStateRows()
	}
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
func (s *Provider) targetStateRows() []providerRow {
	rows := make([]providerRow, 0, len(s.targets))
	lastGroup := ""
	for _, target := range s.targets {
		group := providerTargetGroup(s.id, target)
		if group != lastGroup {
			rows = append(rows, providerRow{group: group})
			lastGroup = group
		}
		label := target.Label
		if label == "" {
			label = target.Key
		}
		rows = append(rows, providerRow{value: fmt.Sprintf("%s — desired %s; current %s", label, target.Desired, target.Current), key: target.Key, target: target})
	}
	return rows
}
func (s *Provider) policyRows() []providerRow {
	rows := make([]providerRow, 0, len(s.targets))
	lastGroup := ""
	for _, target := range s.targets {
		group := providerTargetGroup(s.id, target)
		if group != lastGroup {
			rows = append(rows, providerRow{group: group})
			lastGroup = group
		}
		effective := s.effective[target.Key]
		setting := effective.Capture
		blocked := ""
		if s.tab == "Restore" {
			setting = effective.Restore
			if !target.RestoreEligible {
				blocked = target.SafetyReason
			}
		} else if !target.CaptureEligible {
			blocked = target.SafetyReason
		}
		if blocked == "" {
			if s.tab == "Capture" && !target.CaptureEligible {
				blocked = "not eligible for capture"
			}
			if s.tab == "Restore" && !target.RestoreEligible {
				blocked = "not eligible for restore"
			}
		}
		label := target.Label
		if label == "" {
			label = target.Key
		}
		rows = append(rows, providerRow{value: label + " — " + components.RenderPolicyStatus(s.tab, setting, blocked), key: target.Key, target: target, effective: setting})
	}
	return rows
}
func providerTargetGroup(id string, target workflow.TargetInspection) string {
	if id != "packages" {
		switch id {
		case "themes":
			if target.Key == "active" {
				return "Active theme"
			}
			return "Available themes"
		case "plugins":
			return "Plugins (Shell owns enablement)"
		case "defaults":
			return "Application defaults"
		case "shell":
			return "Shell customization"
		case "hooks":
			return "Hooks"
		case "resources":
			return "Resources (Exact never deletes resource data)"
		default:
			return titleFor(id)
		}
	}
	switch {
	case target.Key == "preinstalls" || strings.HasPrefix(target.Key, "preinstall:"):
		return "Omarchy preinstalls"
	case strings.HasPrefix(target.Key, "official:"):
		return "Official packages"
	case strings.HasPrefix(target.Key, "aur:"):
		return "AUR packages"
	case strings.HasPrefix(target.Key, "mise:"):
		return "Mise tools"
	default:
		return "Other packages"
	}
}
func (s *Provider) groupAnchors() []int {
	rows := s.rows()
	anchors := make([]int, 0, len(rows))
	for index, row := range rows {
		if row.group != "" {
			anchors = append(anchors, index)
		}
	}
	return anchors
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
		// A package/tool with Capture Disabled (excluded) now has no desired
		// state at all -- it is stripped from Official/AUR/Mise immediately
		// (Session.SetPackageExcluded), rather than staying listed with a
		// separate "not included" marker, so every row shown here is simply
		// included.
		packageRows := func(section string, values []string) {
			items := append([]string{}, values...)
			group(section, items, &rows)
			for i := range rows {
				if rows[i].section == section {
					rows[i].state = "included"
				}
			}
		}
		packageRows("Official packages", value.Official)
		packageRows("AUR packages", value.AUR)
		tools := make([]string, 0, len(value.Mise))
		for tool := range value.Mise {
			tools = append(tools, tool)
		}
		sort.Strings(tools)
		packageRows("Mise tools", tools)
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
			themes = append(themes, item.ID+valueSuffix(sourceLabel(item.Type), item.Revision))
		}
		group("Saved themes", themes, &rows)
	case profile.Plugins:
		pluginRows := func(label, source string) {
			plugins := make([]string, 0)
			for _, item := range value.Items {
				if item.Source == source {
					plugins = append(plugins, item.ID+valueSuffix(item.Revision))
				}
			}
			group(label, plugins, &rows)
		}
		pluginRows("Installed plugins", "git")
		pluginRows("Local plugins", "local")
		pluginRows("Built-in plugins", "builtin")
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
func sourceLabel(source string) string {
	switch source {
	case "git":
		return "installed"
	case "builtin":
		return "built-in"
	case "local":
		return "local"
	default:
		return source
	}
}
func (s *Provider) refresh() tea.Cmd {
	s.requestID++
	requestID := s.requestID
	s.busy = true
	return func() tea.Msg {
		report, err := s.session.Status(s.ctx, s.id)
		if err != nil && !providerCaptured(s.session.Profile(), s.id) {
			return providerStatusMsg{requestID: requestID, status: workflow.ProviderStatus{ID: s.id}}
		}
		if err != nil {
			return providerStatusMsg{requestID: requestID, err: err}
		}
		for _, status := range report.Providers {
			if status.ID == s.id {
				targets, err := s.session.PolicyTargets(s.ctx, s.id)
				if err != nil {
					return providerStatusMsg{requestID: requestID, err: err}
				}
				effective := make(map[string]policy.Effective, len(targets))
				scope := workflow.PolicyScope{Machine: s.session.Machine().Name}
				for _, target := range targets {
					resolved, err := s.session.EffectivePolicy(s.ctx, scope, s.id, target)
					if err != nil {
						return providerStatusMsg{requestID: requestID, err: err}
					}
					effective[target.Key] = resolved
				}
				return providerStatusMsg{requestID: requestID, status: status, targets: targets, effective: effective}
			}
		}
		return providerStatusMsg{requestID: requestID, err: fmt.Errorf("%s status is unavailable", s.id)}
	}
}
func (s *Provider) capture() tea.Cmd {
	s.requestID++
	requestID := s.requestID
	return func() tea.Msg {
		_, err := s.session.Capture(s.ctx, s.id)
		var warning workflow.PostCommitWarning
		if errors.As(err, &warning) {
			return providerCaptureMsg{requestID: requestID, warning: warning.Err}
		}
		return providerCaptureMsg{requestID: requestID, err: err}
	}
}
func (s *Provider) CanToggleSelected() bool {
	row := s.selectedSavedRow()
	return !s.busy && s.activeTab() == "Saved" && row.value != "" && providerItemCanToggle(s.id, row.section)
}

// CanSetPolicySelected reports whether the selected target may receive an
// explicit policy override on the active Capture/Restore tab.
func (s *Provider) CanSetPolicySelected() bool {
	return !s.busy && s.policyTab() && s.selectedPolicyRow().key != ""
}
func (s *Provider) ToggleSelectedLabel() string {
	if s.selectedSavedRow().state == "not included" {
		return "Include selected package"
	}
	return "Exclude selected package"
}
func (s *Provider) toggleItem(row providerRow) tea.Cmd {
	s.requestID++
	requestID := s.requestID
	return func() tea.Msg {
		return providerToggleMsg{requestID: requestID, err: s.session.SetProviderItemEnabled(s.ctx, s.id, row.section, row.key)}
	}
}
func (s *Provider) policyScope() workflow.PolicyScope {
	if s.session == nil {
		return workflow.PolicyScope{}
	}
	return workflow.PolicyScope{Machine: s.session.Machine().Name}
}
func (s *Provider) policyAxis() policy.Axis {
	if s.tab == "Restore" {
		return policy.AxisRestore
	}
	return policy.AxisCapture
}
func (s *Provider) setPolicy(row providerRow) tea.Cmd {
	s.requestID++
	requestID := s.requestID
	setting := policy.SettingDisabled
	if !row.effective.Enabled {
		setting = policy.SettingEnabled
	}
	return func() tea.Msg {
		return providerToggleMsg{requestID: requestID, err: s.session.SetPolicy(s.policyScope(), s.policyAxis(), s.id, row.key, setting)}
	}
}
func (s *Provider) clearPolicy(row providerRow) tea.Cmd {
	s.requestID++
	requestID := s.requestID
	return func() tea.Msg {
		return providerToggleMsg{requestID: requestID, err: s.session.ClearPolicy(s.policyScope(), s.policyAxis(), s.id, row.key)}
	}
}
func providerItemCanToggle(id, section string) bool {
	if id == "packages" {
		return section == "Official packages" || section == "AUR packages" || section == "Mise tools"
	}
	return false
}
func providerItemCanToggleID(id string) bool {
	return id == "packages"
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
		return title + " details unavailable: " + components.DisplayText(s.err.Error())
	}
	if s.tab == "Changes" {
		if change := s.selectedChange(); change.Summary != "" {
			return title + " change\n" + components.DisplayText(change.Summary)
		}
	}
	if s.policyTab() {
		if row := s.selectedPolicyRow(); row.key != "" {
			return fmt.Sprintf("%s policy\nDesired: %s\nCurrent: %s\nEffective: %s\nCapture eligible: %t\nRestore eligible: %t", title, row.target.Desired, row.target.Current, components.RenderPolicyStatus(s.tab, row.effective, ""), row.target.CaptureEligible, row.target.RestoreEligible)
		}
		return title + " policy\n" + s.policyScopeLabel()
	}
	if row := s.selectedSavedRow(); row.value != "" {
		return title + " saved state\n" + components.DisplayText(row.value)
	}
	if copy, ok := providerEmptyState(s.id, s.status, "Saved"); ok {
		return title + " saved state\n" + copy.Heading
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
	if s.activeTab() == "Saved" && s.list.Selected >= 0 && s.list.Selected < len(rows) {
		return rows[s.list.Selected]
	}
	return providerRow{}
}
func (s *Provider) selectedPolicyRow() providerRow {
	rows := s.rows()
	if s.policyTab() && s.list.Selected >= 0 && s.list.Selected < len(rows) {
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
