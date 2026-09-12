package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type ScreenID string

const (
	ScreenOverview  ScreenID = "overview"
	ScreenPackages  ScreenID = "packages"
	ScreenConfig    ScreenID = "config"
	ScreenResources ScreenID = "resources"
	ScreenThemes    ScreenID = "themes"
	ScreenPlugins   ScreenID = "plugins"
	ScreenShell     ScreenID = "shell"
	ScreenHooks     ScreenID = "hooks"
	ScreenDefaults  ScreenID = "defaults"
	ScreenMachines  ScreenID = "machines"
	ScreenSync      ScreenID = "sync"
	ScreenRestore   ScreenID = "restore"
)

var screenOrder = []ScreenID{ScreenOverview, ScreenPackages, ScreenConfig, ScreenResources, ScreenThemes, ScreenPlugins, ScreenShell, ScreenHooks, ScreenDefaults, ScreenMachines, ScreenSync, ScreenRestore}

type screen interface {
	ID() ScreenID
	SetSize(width, height int)
	Update(tea.Msg) tea.Cmd
	View() string
	Actions() []Action
}

type focusArea uint8

const (
	focusSidebar focusArea = iota
	focusWorkspace
	focusDetails
)

type model struct {
	palette               Palette
	themeLoader           ThemeLoader
	fingerprint           string
	width, height         int
	selected              int
	focus                 focusArea
	paletteOpen, helpOpen bool
	paletteQuery          string
	paletteSelected       int
	notification          string
	screens               map[ScreenID]screen
}

func newModel(loader ThemeLoader) model {
	return newModelWithSession(loader, nil)
}

func newModelWithSession(loader ThemeLoader, session *workflow.Session) model {
	screenMap := make(map[ScreenID]screen, len(screenOrder))
	for _, id := range screenOrder {
		screenMap[id] = &placeholderScreen{id: id}
	}
	if session != nil {
		screenMap[ScreenOverview] = &overviewScreen{Overview: screens.NewOverview(session)}
		screenMap[ScreenConfig] = &configScreen{Config: screens.NewConfig(session)}
		screenMap[ScreenResources] = &resourcesScreen{Resources: screens.NewResources(session)}
		screenMap[ScreenMachines] = &machinesScreen{Machines: screens.NewMachines(session)}
		screenMap[ScreenRestore] = &restoreScreen{Restore: screens.NewRestore(session)}
		screenMap[ScreenSync] = &syncScreen{Sync: screens.NewSync(session)}
		for _, id := range []ScreenID{ScreenPackages, ScreenThemes, ScreenPlugins, ScreenShell, ScreenHooks, ScreenDefaults} {
			screenMap[id] = &providerScreen{Provider: screens.NewProvider(session, string(id)), id: id}
		}
	}
	return model{palette: loader.Load(), themeLoader: loader, fingerprint: loader.Fingerprint(), screens: screenMap}
}

func (m model) Init() tea.Cmd {
	commands := []tea.Cmd{themeTickCmd()}
	if config, ok := m.screens[ScreenConfig].(*configScreen); ok {
		commands = append(commands, config.Init())
	}
	if resources, ok := m.screens[ScreenResources].(*resourcesScreen); ok {
		commands = append(commands, resources.Init())
	}
	if machines, ok := m.screens[ScreenMachines].(*machinesScreen); ok {
		commands = append(commands, machines.Init())
	}
	if restore, ok := m.screens[ScreenRestore].(*restoreScreen); ok {
		commands = append(commands, restore.Init())
	}
	if sync, ok := m.screens[ScreenSync].(*syncScreen); ok {
		commands = append(commands, sync.Init())
	}
	if overview, ok := m.screens[ScreenOverview].(*overviewScreen); ok {
		commands = append(commands, overview.Init())
	}
	for _, id := range []ScreenID{ScreenPackages, ScreenThemes, ScreenPlugins, ScreenShell, ScreenHooks, ScreenDefaults} {
		if provider, ok := m.screens[id].(*providerScreen); ok {
			commands = append(commands, provider.Init())
		}
	}
	return tea.Batch(commands...)
}

// newInspectPathCmd binds browser inspection to the workflow session without
// making the reusable component depend on session construction.
func newInspectPathCmd(session *workflow.Session) func(uint64, string) tea.Cmd {
	return func(requestID uint64, path string) tea.Cmd {
		return func() tea.Msg {
			inspection, err := session.InspectPath(context.Background(), path)
			return pathInspectedMsg{RequestID: requestID, Inspection: inspection, Err: err}
		}
	}
}

func (m model) activeScreen() screen { return m.screens[m.screenID()] }
func (m model) screenID() ScreenID   { return screenOrder[m.selected] }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if target, ok := msg.(screens.OverviewTarget); ok {
		m.selectScreen(ScreenID(target.Target))
		switch current := m.activeScreen().(type) {
		case *configScreen:
			return m, current.Focus(target.Ref)
		case *resourcesScreen:
			return m, current.Focus(target.Ref)
		case *machinesScreen:
			current.Focus(target.Ref)
		}
		return m, nil
	}
	if complete, ok := msg.(screens.CaptureComplete); ok {
		if complete.Err != nil {
			m.notification = "Capture failed: " + complete.Err.Error()
			return m, nil
		}
		m.notification = "Capture complete. Overview and Profile Git status refreshed."
		commands := []tea.Cmd{}
		if overview, ok := m.screens[ScreenOverview].(*overviewScreen); ok {
			commands = append(commands, overview.Update(tea.KeyPressMsg{Code: 'r'}))
		}
		if sync, ok := m.screens[ScreenSync].(*syncScreen); ok {
			commands = append(commands, sync.Update(tea.KeyPressMsg{Code: 'r'}))
		}
		if provider, ok := m.screens[ScreenID(complete.Provider)].(*providerScreen); ok {
			commands = append(commands, provider.Refresh())
		}
		return m, tea.Batch(commands...)
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		layout := layoutForSize(m.width, m.height)
		for _, current := range m.screens {
			current.SetSize(layout.workspaceWidth, layout.contentHeight)
		}
		return m, nil
	}
	if _, ok := msg.(themeTickMsg); ok {
		fingerprint := m.themeLoader.Fingerprint()
		if fingerprint != m.fingerprint {
			m.palette, m.fingerprint = m.themeLoader.Load(), fingerprint
		}
		return m, themeTickCmd()
	}
	if finished, ok := msg.(handoffFinishedMsg); ok {
		m.notification = "External tool closed. Refreshing " + finished.Kind + "."
		if finished.Err != nil {
			m.notification = "External tool closed with an error. Refreshing " + finished.Kind + "."
		}
		if current := m.activeScreen(); current != nil {
			return m, current.Update(msg)
		}
		return m, nil
	}
	if _, ok := msg.(clipboardCopiedMsg); ok {
		m.notification = "Copy requested."
		return m, nil
	}
	if request, ok := msg.(screens.HandoffRequest); ok {
		handoff := Handoff{Visual: "", Editor: ""}
		switch request.Kind {
		case "copy":
			return m, copyCmd(request.Path)
		case "editor":
			command, err := handoff.EditorCmd(request.Path)
			if err != nil {
				m.notification = err.Error()
				return m, nil
			}
			return m, handoffCmd("config", command)
		case "open":
			command, err := handoff.FileManagerCmd(request.Path, false)
			if err != nil {
				m.notification = err.Error()
				return m, nil
			}
			return m, handoffCmd("config", command)
		case "browser":
			command, err := handoff.BrowserCmd(request.Path)
			if err != nil {
				m.notification = err.Error()
				return m, nil
			}
			return m, handoffCmd("sync", command)
		case "lazygit":
			command, err := handoff.LazyGitCmd(request.Path)
			if err != nil {
				m.notification = err.Error()
				return m, nil
			}
			return m, handoffCmd("sync", command)
		}
	}

	key, isKey := keyName(msg)
	if m.paletteOpen {
		return m.updatePalette(key, isKey)
	}
	if m.helpOpen {
		if isKey && (key == "esc" || key == "?" || key == "q") {
			m.helpOpen = false
		}
		return m, nil
	}
	if isKey {
		switch key {
		case "ctrl+c", "q":
			return m, tea.Quit
		case ":":
			m.paletteOpen, m.paletteQuery, m.paletteSelected = true, "", 0
			return m, nil
		case "?":
			m.helpOpen = true
			return m, nil
		case "tab", "shift+tab":
			m.cycleFocus(isReverseTab(key))
			return m, nil
		case "j", "down":
			if m.focus == focusSidebar {
				m.moveScreen(1)
				return m, nil
			}
		case "k", "up":
			if m.focus == focusSidebar {
				m.moveScreen(-1)
				return m, nil
			}
		case "h", "left":
			if m.focus > focusSidebar {
				m.focus--
				return m, nil
			}
		case "l", "right":
			if m.focus < focusDetails && layoutForSize(m.width, m.height).mode == LayoutThreePane {
				m.focus++
				return m, nil
			}
		}
	}
	if current := m.activeScreen(); current != nil {
		return m, current.Update(msg)
	}
	return m, nil
}

func (m *model) updatePalette(key string, isKey bool) (tea.Model, tea.Cmd) {
	if !isKey {
		return *m, nil
	}
	items := filterActions(m.actions(), m.paletteQuery)
	switch key {
	case "esc":
		m.paletteOpen = false
	case "up", "k":
		if m.paletteSelected > 0 {
			m.paletteSelected--
		}
	case "down", "j":
		if m.paletteSelected < len(items)-1 {
			m.paletteSelected++
		}
	case "enter":
		if len(items) > 0 && items[m.paletteSelected].Enabled {
			action := items[m.paletteSelected]
			m.paletteOpen = false
			if action.Screen != "" {
				m.selectScreen(action.Screen)
			}
			if action.Run != nil {
				return *m, action.Run()
			}
		}
	case "backspace":
		if len(m.paletteQuery) > 0 {
			m.paletteQuery = m.paletteQuery[:len(m.paletteQuery)-1]
		}
	default:
		if len(key) == 1 {
			m.paletteQuery += key
		}
	}
	items = filterActions(m.actions(), m.paletteQuery)
	if m.paletteSelected >= len(items) {
		m.paletteSelected = max(0, len(items)-1)
	}
	return *m, nil
}

func (m *model) moveScreen(delta int) {
	m.selected = (m.selected + delta + len(screenOrder)) % len(screenOrder)
}
func (m *model) selectScreen(id ScreenID) {
	for i, candidate := range screenOrder {
		if candidate == id {
			m.selected, m.focus = i, focusWorkspace
			return
		}
	}
}
func (m *model) cycleFocus(reverse bool) {
	count := 2
	if layoutForSize(m.width, m.height).mode == LayoutThreePane {
		count = 3
	}
	delta := 1
	if reverse {
		delta = -1
	}
	m.focus = focusArea((int(m.focus) + delta + count) % count)
}

func (m model) actions() []Action {
	actions := make([]Action, 0, len(screenOrder)+4)
	for _, id := range screenOrder {
		actions = append(actions, Action{ID: "screen." + string(id), Label: screenLabel(id), Enabled: true, Screen: id})
	}
	actions = append(actions, Action{ID: "help", Label: "Show help", Shortcut: "?", Enabled: true}, Action{ID: "quit", Label: "Quit", Shortcut: "q", Enabled: true, Run: func() tea.Cmd { return tea.Quit }})
	if current := m.activeScreen(); current != nil {
		actions = append(actions, current.Actions()...)
	}
	return actions
}

func (m model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		view := tea.NewView("Omarchy Blueprint")
		view.AltScreen = true
		return view
	}
	layout := layoutForSize(m.width, m.height)
	if layout.mode == LayoutTooSmall {
		view := tea.NewView(fmt.Sprintf("Terminal too small (%dx%d). Need at least 70 columns and 18 rows.", m.width, m.height))
		view.AltScreen = true
		return view
	}
	header := m.accent("Blueprint") + "  profile: default  machine: portable default  " + m.warning("! attention")
	footer := m.muted(components.Statusbar(statusActions(m.activeScreen())))
	if m.notification != "" {
		footer += "  " + m.notification
	}
	content := m.contentView(layout)
	if m.paletteOpen {
		content = components.Palette(m.paletteQuery, paletteItems(filterActions(m.actions(), m.paletteQuery)), m.paletteSelected)
	}
	if m.helpOpen {
		content = components.Help(screenLabel(m.screenID()), statusActions(m.activeScreen()))
	}
	view := tea.NewView(strings.Join([]string{header, strings.Repeat("─", max(1, m.width)), content, strings.Repeat("─", max(1, m.width)), footer}, "\n"))
	view.AltScreen = true
	return view
}

func (m model) accent(value string) string  { return m.color(value, m.palette.Accent) }
func (m model) warning(value string) string { return m.color(value, m.palette.Warning) }
func (m model) muted(value string) string   { return m.color(value, m.palette.Muted) }
func (m model) color(value, color string) string {
	if !m.palette.ColorEnabled || color == "" {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(value)
}

func (m model) contentView(layout layout) string {
	items := make([]components.NavItem, 0, len(screenOrder))
	for _, id := range screenOrder {
		items = append(items, components.NavItem{ID: string(id), Label: screenLabel(id)})
	}
	workspace := m.activeScreen().View()
	if layout.mode == LayoutCompact {
		return workspace
	}
	sidebar := components.Sidebar(items, m.selected, layout.sidebarWidth)
	if layout.mode == LayoutTwoPane {
		return joinColumns(sidebar, workspace, layout.sidebarWidth, layout.workspaceWidth)
	}
	return joinColumns(sidebar, workspace, "Details / preview", layout.sidebarWidth, layout.workspaceWidth, layout.detailsWidth)
}

func joinColumns(values ...interface{}) string {
	// Layout widths accompany the column contents but are not rendered here.
	columns := make([]string, 0, len(values))
	for _, value := range values {
		if column, ok := value.(string); ok {
			columns = append(columns, column)
		}
	}
	return strings.Join(columns, " │ ")
}

func statusActions(current screen) []components.StatusAction {
	actions := []components.StatusAction{}
	if current != nil {
		for _, action := range current.Actions() {
			actions = append(actions, components.StatusAction{Label: action.Label, Shortcut: action.Shortcut, Enabled: action.Enabled})
		}
	}
	return actions
}

func paletteItems(actions []Action) []components.PaletteItem {
	items := make([]components.PaletteItem, 0, len(actions))
	for _, action := range actions {
		items = append(items, components.PaletteItem{ID: action.ID, Label: action.Label, Shortcut: action.Shortcut, Enabled: action.Enabled, DisabledReason: action.DisabledReason})
	}
	return items
}

func screenLabel(id ScreenID) string {
	return strings.ToUpper(string(id[:1])) + strings.ReplaceAll(string(id[1:]), "-", " ")
}

type placeholderScreen struct {
	id            ScreenID
	width, height int
}

func (s *placeholderScreen) ID() ScreenID              { return s.id }
func (s *placeholderScreen) SetSize(width, height int) { s.width, s.height = width, height }
func (s *placeholderScreen) Update(tea.Msg) tea.Cmd    { return nil }
func (s *placeholderScreen) View() string              { return screenLabel(s.id) + "\n\n✓ Screen is ready." }
func (s *placeholderScreen) Actions() []Action         { return nil }

type configScreen struct{ *screens.Config }

type overviewScreen struct{ *screens.Overview }

type resourcesScreen struct{ *screens.Resources }

type machinesScreen struct{ *screens.Machines }

type restoreScreen struct{ *screens.Restore }

type syncScreen struct{ *screens.Sync }

type providerScreen struct {
	*screens.Provider
	id ScreenID
}

func (s *resourcesScreen) ID() ScreenID { return ScreenResources }
func (s *resourcesScreen) Actions() []Action {
	return []Action{
		{ID: "resources.discover", Label: "Discover resource", Shortcut: "n", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'n'}) }},
		{ID: "resources.untrack", Label: "Untrack resource", Shortcut: "x", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'x'}) }},
	}
}

func (s *machinesScreen) ID() ScreenID { return ScreenMachines }
func (s *machinesScreen) Actions() []Action {
	return []Action{
		{ID: "machines.add", Label: "Add machine", Shortcut: "a", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'a'}) }},
		{ID: "machines.map", Label: "Map resource directory", Shortcut: "m", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'm'}) }},
	}
}

func (s *restoreScreen) ID() ScreenID { return ScreenRestore }
func (s *restoreScreen) Actions() []Action {
	return []Action{{ID: "restore.force", Label: "Toggle forced restore", Shortcut: "f", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'f'}) }}, {ID: "restore.apply", Label: "Apply restore", Shortcut: "enter", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) }}}
}

func (s *syncScreen) ID() ScreenID { return ScreenSync }
func (s *syncScreen) Actions() []Action {
	states := s.Sync.Actions()
	actions := make([]Action, 0, len(states))
	for _, state := range states {
		state := state
		actions = append(actions, Action{ID: state.ID, Label: state.Label, Shortcut: state.Shortcut, Enabled: state.Enabled, DisabledReason: state.DisabledReason, Run: func() tea.Cmd {
			return s.Update(tea.KeyPressMsg{Code: rune(state.Shortcut[0])})
		}})
	}
	return actions
}

func (s *providerScreen) ID() ScreenID { return s.id }
func (s *providerScreen) Actions() []Action {
	return []Action{{ID: string(s.id) + ".capture", Label: "Capture " + screenLabel(s.id), Shortcut: "c", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'c'}) }}}
}

func (s *configScreen) ID() ScreenID { return ScreenConfig }
func (s *configScreen) Actions() []Action {
	return []Action{
		{ID: "config.diff", Label: "View Config diff", Shortcut: "d", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'd'}) }},
		{ID: "config.include", Label: "Include Config path", Shortcut: "i", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'i'}) }},
		{ID: "config.exclude", Label: "Exclude Config path", Shortcut: "x", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'x'}) }},
		{ID: "config.auto", Label: "Use automatic Config policy", Shortcut: "a", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'a'}) }},
		{ID: "config.edit", Label: "Edit Config file", Shortcut: "e", Enabled: s.CanHandoff(), DisabledReason: "selected Config path is not a regular live file", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'e'}) }},
		{ID: "config.open", Label: "Open Config location", Shortcut: "o", Enabled: s.CanHandoff(), DisabledReason: "selected Config path is not a regular live file", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'o'}) }},
		{ID: "config.copy", Label: "Copy Config path", Shortcut: "y", Enabled: s.CanHandoff(), DisabledReason: "selected Config path is not a regular live file", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'y'}) }},
	}
}

func (s *overviewScreen) ID() ScreenID { return ScreenOverview }
func (s *overviewScreen) Actions() []Action {
	return []Action{{ID: "overview.refresh", Label: "Refresh Overview", Shortcut: "r", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'r'}) }}}
}
