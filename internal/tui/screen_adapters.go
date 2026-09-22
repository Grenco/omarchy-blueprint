package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
)

// screen is the root-facing contract every workspace screen must satisfy.
type screen interface {
	ID() ScreenID
	SetSize(width, height int)
	Update(tea.Msg) tea.Cmd
	View() string
	Actions() []Action
}

type initializableScreen interface{ Init() tea.Cmd }
type transientScreen interface{ TransientActive() bool }
type styleableScreen interface{ SetStyles(components.Styles) }
type terminalWidthScreen interface{ SetTerminalWidth(int) }
type headerStateScreen interface{ HeaderState() string }
type bindingScreen interface{ Bindings() []Binding }

// DetailView is optional: screens that provide it own the third pane preview.
type DetailView interface{ DetailView() string }

type placeholderScreen struct {
	id            ScreenID
	width, height int
}

func (s *placeholderScreen) ID() ScreenID              { return s.id }
func (s *placeholderScreen) SetSize(width, height int) { s.width, s.height = width, height }
func (s *placeholderScreen) Update(tea.Msg) tea.Cmd    { return nil }
func (s *placeholderScreen) View() string              { return screenLabel(s.id) + "\n\n✓ Screen is ready." }
func (s *placeholderScreen) DetailView() string {
	return screenLabel(s.id) + " details\nThis screen is ready when a profile session is available."
}
func (s *placeholderScreen) Actions() []Action { return nil }

type configScreen struct{ *screens.Config }

type overviewScreen struct{ *screens.Overview }

type captureScreen struct{ *screens.Capture }

type resourcesScreen struct{ *screens.Resources }

type machinesScreen struct{ *screens.Machines }

type restoreScreen struct{ *screens.Restore }

type syncScreen struct{ *screens.Sync }

type providerScreen struct {
	*screens.Provider
	id ScreenID
}

func (s *resourcesScreen) ID() ScreenID  { return ScreenResources }
func (s *resourcesScreen) OwnsTab() bool { return !s.TransientActive() }
func (s *resourcesScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !s.TransientActive() && !screenKey(key.String()) && key.String() != "tab" && key.String() != "shift+tab" {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *resourcesScreen) Actions() []Action {
	states := s.Resources.Actions()
	actions := make([]Action, 0, len(states))
	for _, state := range states {
		state := state
		actions = append(actions, Action{ID: "resources." + state.ID, Label: state.Label, Group: "Resources", Enabled: state.Enabled, Visible: true, DisabledReason: state.DisabledReason, Run: func() tea.Cmd {
			key := tea.KeyPressMsg{Code: rune(state.Shortcut[0])}
			switch state.Shortcut {
			case "space":
				key.Code = tea.KeySpace
			case "enter":
				key.Code = tea.KeyEnter
			case "tab":
				key.Code = tea.KeyTab
			}
			return s.Update(key)
		}})
	}
	return actions
}
func (s *resourcesScreen) Bindings() []Binding {
	return bindingsFromResourceActions(s.Resources.Actions(), "resources.")
}

func (s *machinesScreen) ID() ScreenID { return ScreenMachines }
func (s *machinesScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !s.TransientActive() && !s.Machines.OwnsWorkspaceKey(key.String()) {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *machinesScreen) Actions() []Action {
	selectedMachine := s.HasSelectedMachine()
	primaryLabel, primaryEnabled, primaryReason := "Use machine overlay", s.CanUseMachine(), "selected machine is already active"
	if s.MappingFocused() {
		primaryLabel, primaryEnabled, primaryReason = "Remove resource mapping", s.CanUnmapResource(), "selected resource has no override"
	}
	return []Action{
		{ID: "machines.add", Label: "Add machine", Group: "Machines", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'a'}) }},
		{ID: "machines.primary", Label: primaryLabel, Group: "Machines", Enabled: primaryEnabled, Visible: true, DisabledReason: primaryReason, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'u'}) }},
		{ID: "machines.clear", Label: "Clear active machine", Group: "Machines", Enabled: s.CanClearMachine(), Visible: true, DisabledReason: "no active machine", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'c'}) }},
		{ID: "machines.rename", Label: "Rename machine", Group: "Machines", Enabled: selectedMachine, Visible: true, DisabledReason: "select a machine", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'r'}) }},
		{ID: "machines.restore-conflicts", Label: "Toggle Safe/Force default", Group: "Machines", Enabled: selectedMachine, Visible: true, DisabledReason: "select a machine", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'f'}) }},
		{ID: "machines.restore-convergence", Label: "Toggle Additive/Exact default", Group: "Machines", Enabled: selectedMachine, Visible: true, DisabledReason: "select a machine", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'e'}) }},
		{ID: "machines.previous-policy", Label: "Select previous policy category", Group: "Machines", Enabled: s.CanCyclePolicy(), Visible: true, DisabledReason: "selected machine has fewer than two policy categories", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: '['}) }},
		{ID: "machines.next-policy", Label: "Select next policy category", Group: "Machines", Enabled: s.CanCyclePolicy(), Visible: true, DisabledReason: "selected machine has fewer than two policy categories", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: ']'}) }},
		{ID: "machines.open-policy", Label: "Open selected policy category", Group: "Machines", Enabled: s.CanOpenPolicy(), Visible: true, DisabledReason: "selected machine has no policy overrides", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'o'}) }},
		{ID: "machines.remove", Label: "Remove machine", Group: "Machines", Enabled: selectedMachine, Visible: true, DisabledReason: "select a machine", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'x'}) }},
		{ID: "machines.map", Label: "Map resource directory", Group: "Machines", Enabled: s.CanMapResource(), Visible: true, DisabledReason: "select a resource path", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'm'}) }},
	}
}
func (s *machinesScreen) Bindings() []Binding {
	return []Binding{{ActionID: "machines.add", Key: "a"}, {ActionID: "machines.primary", Key: "u"}, {ActionID: "machines.clear", Key: "c"}, {ActionID: "machines.rename", Key: "r"}, {ActionID: "machines.restore-conflicts", Key: "f"}, {ActionID: "machines.restore-convergence", Key: "e"}, {ActionID: "machines.previous-policy", Key: "["}, {ActionID: "machines.next-policy", Key: "]"}, {ActionID: "machines.open-policy", Key: "o"}, {ActionID: "machines.remove", Key: "x"}, {ActionID: "machines.map", Key: "m"}}
}

func (s *restoreScreen) ID() ScreenID { return ScreenRestore }
func (s *restoreScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !s.TransientActive() && !screenKey(key.String()) {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *restoreScreen) Actions() []Action {
	return []Action{{ID: "restore.conflicts", Label: "Toggle Safe/Force for this run", Group: "Restore", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'f'}) }}, {ID: "restore.convergence", Label: "Toggle Additive/Exact for this run", Group: "Restore", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'e'}) }}, {ID: "restore.apply", Label: "Restore current plan", Group: "Restore", Enabled: s.CanApply(), Visible: true, DisabledReason: "current restore plan has no operations", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) }}}
}
func (s *restoreScreen) Bindings() []Binding {
	return []Binding{{ActionID: "restore.conflicts", Key: "f"}, {ActionID: "restore.convergence", Key: "e"}, {ActionID: "restore.apply", Key: "enter"}}
}

func (s *syncScreen) ID() ScreenID { return ScreenSync }
func (s *syncScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !s.TransientActive() && !screenKey(key.String()) {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *syncScreen) Actions() []Action {
	states := s.Sync.Actions()
	actions := make([]Action, 0, len(states))
	for _, state := range states {
		state := state
		actions = append(actions, Action{ID: state.ID, Label: state.Label, Enabled: state.Enabled, Visible: true, DisabledReason: state.DisabledReason, Run: func() tea.Cmd {
			return s.Update(tea.KeyPressMsg{Code: rune(state.Shortcut[0])})
		}})
	}
	return actions
}
func (s *syncScreen) Bindings() []Binding { return bindingsFromSyncActions(s.Sync.Actions()) }

func (s *providerScreen) ID() ScreenID  { return s.id }
func (s *providerScreen) OwnsTab() bool { return !s.TransientActive() }
func (s *providerScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !screenKey(key.String()) && key.String() != "tab" && key.String() != "shift+tab" {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *providerScreen) Actions() []Action {
	actions := []Action{
		{ID: string(s.id) + ".tab", Label: "Switch policy tab", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeyTab}) }},
		{ID: string(s.id) + ".refresh", Label: "Refresh " + screenLabel(s.id), Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'r'}) }},
		{ID: string(s.id) + ".capture", Label: "Capture " + screenLabel(s.id), Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'c'}) }},
	}
	if s.id == ScreenPackages {
		actions = append(actions, Action{ID: string(s.id) + ".toggle", Label: s.ToggleSelectedLabel(), Enabled: s.CanToggleSelected(), Visible: true, DisabledReason: "select an Official, AUR, or Mise package", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeySpace}) }})
	}
	if s.Provider.PolicyTab() {
		actions = append(actions, Action{ID: string(s.id) + ".policy-scope", Label: "Toggle Profile defaults / active machine", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'p'}) }})
	}
	if s.Provider.CanSetPolicySelected() {
		actions = append(actions,
			Action{ID: string(s.id) + ".policy", Label: "Change selected policy", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeySpace}) }},
			Action{ID: string(s.id) + ".policy-reset", Label: "Reset selected policy", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'x'}) }},
		)
	}
	return actions
}
func (s *providerScreen) Bindings() []Binding {
	bindings := []Binding{{ActionID: string(s.id) + ".tab", Key: "tab"}, {ActionID: string(s.id) + ".refresh", Key: "r"}, {ActionID: string(s.id) + ".capture", Key: "c"}, {Label: "Collapse group", Key: "enter"}, {Label: "Previous group", Key: "[", HideFromFooter: true}, {Label: "Next group", Key: "]", HideFromFooter: true}}
	if s.id == ScreenPackages {
		bindings = append(bindings, Binding{ActionID: string(s.id) + ".toggle", Key: "space"})
	}
	if s.Provider.PolicyTab() {
		bindings = append(bindings, Binding{ActionID: string(s.id) + ".policy-scope", Key: "p"})
	}
	if s.Provider.CanSetPolicySelected() {
		bindings = append(bindings, Binding{ActionID: string(s.id) + ".policy", Key: "space"}, Binding{ActionID: string(s.id) + ".policy-reset", Key: "x"})
	}
	return bindings
}

func (s *configScreen) ID() ScreenID  { return ScreenConfig }
func (s *configScreen) OwnsTab() bool { return !s.TransientActive() }
func (s *configScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !s.TransientActive() && !screenKey(key.String()) && key.String() != "/" && key.String() != "tab" && key.String() != "shift+tab" {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *configScreen) Actions() []Action {
	policyEnabled := s.CanPolicy()
	if s.PolicyTab() {
		return []Action{
			{ID: "config.tab", Label: "Switch policy tab", Group: "Config", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeyTab}) }},
			{ID: "config.policy-scope", Label: "Toggle Profile defaults / active machine", Group: "Config", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'p'}) }},
			{ID: "config.policy-set", Label: "Change selected policy", Group: "Config", Enabled: s.CanPolicyTarget(), Visible: true, DisabledReason: "select a Config policy target", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeySpace}) }},
			{ID: "config.policy-reset", Label: "Reset selected policy", Group: "Config", Enabled: s.CanPolicyTarget(), Visible: true, DisabledReason: "select a Config policy target", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'x'}) }},
		}
	}
	return []Action{
		{ID: "config.tab", Label: "Switch policy tab", Group: "Config", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeyTab}) }},
		{ID: "config.diff", Label: "View Config diff", Group: "Config", Enabled: policyEnabled, Visible: true, DisabledReason: "select a Config path", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'd'}) }},
		{ID: "config.policy", Label: "Cycle Config policy", Group: "Config", Enabled: policyEnabled, Visible: true, DisabledReason: "select a Config path", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeySpace}) }},
		{ID: "config.include", Label: "Include Config path", Group: "Config", Enabled: policyEnabled, Visible: true, DisabledReason: "select a Config path", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'i'}) }},
		{ID: "config.exclude", Label: "Exclude Config path", Group: "Config", Enabled: policyEnabled, Visible: true, DisabledReason: "select a Config path", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'x'}) }},
		{ID: "config.auto", Label: "Use automatic Config policy", Group: "Config", Enabled: policyEnabled, Visible: true, DisabledReason: "select a Config path", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'a'}) }},
		{ID: "config.edit", Label: "Edit Config file", Group: "Config", Enabled: s.CanHandoff(), Visible: true, DisabledReason: "selected Config path is not a regular live file", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'e'}) }},
		{ID: "config.open", Label: "Open Config location", Group: "Config", Enabled: s.CanHandoff(), Visible: true, DisabledReason: "selected Config path is not a regular live file", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'o'}) }},
		{ID: "config.copy", Label: "Copy Config path", Group: "Config", Enabled: s.CanHandoff(), Visible: true, DisabledReason: "selected Config path is not a regular live file", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'y'}) }},
	}
}
func (s *configScreen) Bindings() []Binding {
	if s.PolicyTab() {
		return []Binding{{ActionID: "config.tab", Key: "tab"}, {ActionID: "config.policy-scope", Key: "p"}, {ActionID: "config.policy-set", Key: "space"}, {ActionID: "config.policy-reset", Key: "x"}}
	}
	return []Binding{{ActionID: "config.tab", Key: "tab"}, {ActionID: "config.policy", Key: "space"}, {ActionID: "config.diff", Key: "d"}, {ActionID: "config.include", Key: "i"}, {ActionID: "config.exclude", Key: "x"}, {ActionID: "config.auto", Key: "a"}, {ActionID: "config.edit", Key: "e"}, {ActionID: "config.open", Key: "o"}, {ActionID: "config.copy", Key: "y"}, {Label: "Previous group", Key: "[", HideFromFooter: true}, {Label: "Next group", Key: "]", HideFromFooter: true}}
}

func (s *overviewScreen) ID() ScreenID { return ScreenOverview }
func (s *captureScreen) ID() ScreenID  { return ScreenCapture }
func (s *captureScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !s.TransientActive() && !screenKey(key.String()) {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *captureScreen) Actions() []Action {
	return []Action{
		{ID: "capture.select", Label: "Toggle category selection", Group: "Capture", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeySpace}) }},
		{ID: "capture.select-all", Label: "Select changed categories", Group: "Capture", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'a'}) }},
		{ID: "capture.run", Label: "Capture selected categories", Group: "Capture", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'c'}) }},
		{ID: "capture.capture-all", Label: "Capture all categories", Group: "Capture", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'C'}) }},
		{ID: "capture.refresh", Label: "Refresh capture status", Group: "Capture", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'r'}) }},
	}
}
func (s *captureScreen) Bindings() []Binding {
	return []Binding{{ActionID: "capture.select", Key: "space"}, {ActionID: "capture.select-all", Key: "a"}, {ActionID: "capture.run", Key: "c"}, {ActionID: "capture.capture-all", Key: "shift+c"}, {ActionID: "capture.refresh", Key: "r"}}
}
func (s *overviewScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !screenKey(key.String()) {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *overviewScreen) Actions() []Action {
	return []Action{
		{ID: "overview.open", Label: "Open selected decision", Enabled: s.CanOpen(), Visible: true, DisabledReason: "select an attention item", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) }},
		{ID: "overview.refresh", Label: "Refresh Overview", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'r'}) }},
	}
}
func (s *overviewScreen) Bindings() []Binding {
	return []Binding{{ActionID: "overview.open", Key: "enter"}, {ActionID: "overview.refresh", Key: "r"}, {Label: "Previous group", Key: "[", HideFromFooter: true}, {Label: "Next group", Key: "]", HideFromFooter: true}}
}

func bindingsFromResourceActions(actions []screens.ResourceAction, prefix string) []Binding {
	bindings := make([]Binding, 0, len(actions))
	for _, action := range actions {
		bindings = append(bindings, Binding{ActionID: prefix + action.ID, Key: action.Shortcut})
	}
	return bindings
}
func bindingsFromSyncActions(actions []screens.SyncAction) []Binding {
	bindings := make([]Binding, 0, len(actions))
	for _, action := range actions {
		bindings = append(bindings, Binding{ActionID: action.ID, Key: action.Shortcut})
	}
	return bindings
}

func screenKey(key string) bool {
	switch key {
	case ":", "?", "q", "tab", "shift+tab", "h", "left", "l", "right":
		return false
	}
	return true
}
