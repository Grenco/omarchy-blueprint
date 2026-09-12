package tui

import (
	"context"
	"fmt"
	"os"
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

type initializableScreen interface{ Init() tea.Cmd }
type transientScreen interface{ TransientActive() bool }
type keyHandlingScreen interface{ HandlesKey(string) bool }
type styleableScreen interface{ SetStyles(components.Styles) }
type headerStateScreen interface{ HeaderState() string }

// DetailView is optional: screens that provide it own the third pane preview.
type DetailView interface{ DetailView() string }

type modalKind uint8

const (
	modalNone modalKind = iota
	modalPalette
	modalHelp
)

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
	modal                 modalKind
	paletteOpen, helpOpen bool // Kept for package-local compatibility; modal is canonical.
	paletteQuery          string
	paletteSelected       int
	sidebarOpen           bool
	sidebarScroll         verticalViewport
	paletteScroll         verticalViewport
	helpScroll            verticalViewport
	notification          string
	screens               map[ScreenID]screen
	session               *workflow.Session
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
	palette := loader.Load()
	for _, current := range screenMap {
		if styled, ok := current.(styleableScreen); ok {
			styled.SetStyles(components.NewStyles(palette))
		}
	}
	return model{palette: palette, themeLoader: loader, fingerprint: loader.Fingerprint(), screens: screenMap, session: session}
}

func (m model) Init() tea.Cmd {
	commands := []tea.Cmd{themeTickCmd()}
	for id, current := range m.screens {
		if initializable, ok := current.(initializableScreen); ok {
			commands = append(commands, wrapScreenCmd(id, initializable.Init()))
		}
	}
	return tea.Batch(commands...)
}

func wrapScreenCmd(id ScreenID, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return screenMsg{Screen: id, Msg: cmd()} }
}

func (m model) updateScreenMsg(wrapped screenMsg) (tea.Model, tea.Cmd) {
	// Root-owned requests are still identified by their originating screen.
	switch msg := wrapped.Msg.(type) {
	case showHelpMsg:
		m.openModal(modalHelp)
		return m, nil
	case screens.OverviewTarget:
		m.selectScreen(ScreenID(msg.Target))
		switch current := m.activeScreen().(type) {
		case *configScreen:
			return m, wrapScreenCmd(m.screenID(), current.Focus(msg.Ref))
		case *resourcesScreen:
			return m, wrapScreenCmd(m.screenID(), current.Focus(msg.Ref))
		case *machinesScreen:
			current.Focus(msg.Ref)
		}
		return m, nil
	case screens.CaptureComplete:
		return m.handleCaptureComplete(msg)
	case screens.HandoffRequest:
		return m.handleHandoffRequest(msg)
	case handoffFinishedMsg:
		m.notification = "External tool closed. Refreshing " + msg.Kind + "."
		if msg.Err != nil {
			m.notification = "External tool closed with an error. Refreshing " + msg.Kind + "."
		}
		if current := m.screens[msg.Refresh]; current != nil {
			return m, wrapScreenCmd(msg.Refresh, current.Update(tea.KeyPressMsg{Code: 'r'}))
		}
	case screens.MachineMutationComplete:
		if msg.Err == nil {
			m.notification = msg.Notice
		} else {
			m.notification = "Machine update failed: " + msg.Err.Error()
		}
	}
	if current := m.screens[wrapped.Screen]; current != nil {
		return m, wrapScreenCmd(wrapped.Screen, current.Update(wrapped.Msg))
	}
	return m, nil
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
	if wrapped, ok := msg.(screenMsg); ok {
		return m.updateScreenMsg(wrapped)
	}
	if _, ok := msg.(showHelpMsg); ok {
		m.openModal(modalHelp)
		return m, nil
	}
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
		if layout.mode == LayoutCompact {
			m.focus, m.sidebarOpen = focusWorkspace, false
		}
		for _, current := range m.screens {
			current.SetSize(layout.workspaceWidth, layout.contentHeight)
		}
		return m, nil
	}
	if _, ok := msg.(themeTickMsg); ok {
		fingerprint := m.themeLoader.Fingerprint()
		if fingerprint != m.fingerprint {
			m.palette, m.fingerprint = m.themeLoader.Load(), fingerprint
			for _, current := range m.screens {
				if styled, ok := current.(styleableScreen); ok {
					styled.SetStyles(components.NewStyles(m.palette))
				}
			}
		}
		return m, themeTickCmd()
	}
	if finished, ok := msg.(handoffFinishedMsg); ok {
		m.notification = "External tool closed. Refreshing " + finished.Kind + "."
		if finished.Err != nil {
			m.notification = "External tool closed with an error. Refreshing " + finished.Kind + "."
		}
		if current := m.screens[finished.Refresh]; current != nil {
			return m, wrapScreenCmd(finished.Refresh, current.Update(tea.KeyPressMsg{Code: 'r'}))
		}
		return m, nil
	}
	if _, ok := msg.(clipboardCopiedMsg); ok {
		m.notification = "Copy requested."
		return m, nil
	}
	if request, ok := msg.(screens.HandoffRequest); ok {
		handoff := Handoff{Visual: os.Getenv("VISUAL"), Editor: os.Getenv("EDITOR")}
		switch request.Kind {
		case "copy":
			return m, copyCmd(request.Path)
		case "editor":
			command, err := handoff.EditorCmd(request.Path)
			if err != nil {
				m.notification = err.Error()
				return m, nil
			}
			return m, handoffCmd(ScreenID(request.Refresh), request.Kind, command)
		case "open":
			command, err := handoff.FileManagerCmd(request.Path, request.IsDir)
			if err != nil {
				m.notification = err.Error()
				return m, nil
			}
			return m, handoffCmd(ScreenID(request.Refresh), request.Kind, command)
		case "browser":
			command, err := handoff.BrowserCmd(request.Path)
			if err != nil {
				m.notification = err.Error()
				return m, nil
			}
			return m, handoffCmd(ScreenID(request.Refresh), request.Kind, command)
		case "lazygit":
			command, err := handoff.LazyGitCmd(request.Path)
			if err != nil {
				m.notification = err.Error()
				return m, nil
			}
			return m, handoffCmd(ScreenID(request.Refresh), request.Kind, command)
		}
	}

	key, isKey := keyName(msg)
	if isKey && key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.modal == modalPalette {
		return m.updatePalette(key, isKey)
	}
	if m.modal == modalHelp {
		if isKey && (key == "esc" || key == "?" || key == "q") {
			m.closeModal()
		}
		if isKey && (key == "j" || key == "down") {
			m.helpScroll.move(1, len(m.helpLines()), layoutForSize(m.width, m.height).contentHeight-2)
		}
		if isKey && (key == "k" || key == "up") {
			m.helpScroll.move(-1, len(m.helpLines()), layoutForSize(m.width, m.height).contentHeight-2)
		}
		return m, nil
	}
	if isKey && m.focus != focusSidebar {
		// Give screen-local text, confirmations, and transient views priority.
		if current := m.activeScreen(); current != nil {
			transient := false
			if owner, ok := current.(transientScreen); ok {
				transient = owner.TransientActive()
			}
			handled := false
			if owner, ok := current.(keyHandlingScreen); ok {
				handled = owner.HandlesKey(key)
			}
			if cmd := current.Update(msg); cmd != nil {
				return m, wrapScreenCmd(m.screenID(), cmd)
			}
			if transient || handled {
				return m, nil
			}
		}
	}
	if isKey {
		switch key {
		case "q":
			if m.focus == focusSidebar {
				return m, tea.Quit
			}
		case ":":
			m.openModal(modalPalette)
			m.paletteQuery, m.paletteSelected = "", 0
			return m, nil
		case "?":
			m.openModal(modalHelp)
			return m, nil
		case "tab", "shift+tab":
			if layoutForSize(m.width, m.height).mode == LayoutCompact {
				m.sidebarOpen = !m.sidebarOpen
				return m, nil
			}
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
		return m, wrapScreenCmd(m.screenID(), current.Update(msg))
	}
	return m, nil
}

func (m model) handleCaptureComplete(complete screens.CaptureComplete) (tea.Model, tea.Cmd) {
	if complete.Err != nil {
		m.notification = "Capture failed: " + complete.Err.Error()
		return m, nil
	}
	m.notification = "Capture complete. Overview and Profile Git status refreshed."
	commands := []tea.Cmd{}
	if overview, ok := m.screens[ScreenOverview].(*overviewScreen); ok {
		commands = append(commands, wrapScreenCmd(ScreenOverview, overview.Update(tea.KeyPressMsg{Code: 'r'})))
	}
	if sync, ok := m.screens[ScreenSync].(*syncScreen); ok {
		commands = append(commands, wrapScreenCmd(ScreenSync, sync.Update(tea.KeyPressMsg{Code: 'r'})))
	}
	if provider, ok := m.screens[ScreenID(complete.Provider)].(*providerScreen); ok {
		commands = append(commands, wrapScreenCmd(ScreenID(complete.Provider), provider.Refresh()))
	}
	return m, tea.Batch(commands...)
}

func (m model) handleHandoffRequest(request screens.HandoffRequest) (tea.Model, tea.Cmd) {
	handoff := Handoff{Visual: os.Getenv("VISUAL"), Editor: os.Getenv("EDITOR")}
	switch request.Kind {
	case "copy":
		return m, copyCmd(request.Path)
	case "editor":
		command, err := handoff.EditorCmd(request.Path)
		if err != nil {
			m.notification = err.Error()
			return m, nil
		}
		return m, handoffCmd(ScreenID(request.Refresh), request.Kind, command)
	case "open":
		command, err := handoff.FileManagerCmd(request.Path, request.IsDir)
		if err != nil {
			m.notification = err.Error()
			return m, nil
		}
		return m, handoffCmd(ScreenID(request.Refresh), request.Kind, command)
	case "browser":
		command, err := handoff.BrowserCmd(request.Path)
		if err != nil {
			m.notification = err.Error()
			return m, nil
		}
		return m, handoffCmd(ScreenID(request.Refresh), request.Kind, command)
	case "lazygit":
		command, err := handoff.LazyGitCmd(request.Path)
		if err != nil {
			m.notification = err.Error()
			return m, nil
		}
		return m, handoffCmd(ScreenID(request.Refresh), request.Kind, command)
	}
	return m, nil
}

func (m *model) openModal(kind modalKind) {
	m.modal = kind
	m.paletteOpen, m.helpOpen = kind == modalPalette, kind == modalHelp
}

func (m *model) closeModal() {
	m.modal, m.paletteOpen, m.helpOpen = modalNone, false, false
}

func (m *model) updatePalette(key string, isKey bool) (tea.Model, tea.Cmd) {
	if !isKey {
		return *m, nil
	}
	items := filterActions(m.actions(), m.paletteQuery)
	switch key {
	case "esc":
		m.closeModal()
	case "up", "k":
		if m.paletteSelected > 0 {
			m.paletteSelected--
		}
	case "down", "j":
		if m.paletteSelected < len(items)-1 {
			m.paletteSelected++
		}
		m.paletteScroll.ensure(m.paletteSelected, len(items), layoutForSize(m.width, m.height).contentHeight-2)
	case "enter":
		if len(items) > 0 && items[m.paletteSelected].Enabled {
			action := items[m.paletteSelected]
			m.closeModal()
			if action.Screen != "" {
				m.selectScreen(action.Screen)
			}
			if action.Run != nil {
				if action.Screen == "" {
					return *m, action.Run()
				}
				return *m, wrapScreenCmd(action.Screen, action.Run())
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
	m.sidebarScroll.ensure(m.selected, len(screenOrder), layoutForSize(m.width, m.height).contentHeight)
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
	if current := m.activeScreen(); current != nil {
		for _, action := range current.Actions() {
			action.Screen = m.screenID()
			if action.Group == "" {
				action.Group = "Actions"
			}
			actions = append(actions, action)
		}
	}
	for _, id := range screenOrder {
		actions = append(actions, Action{ID: "screen." + string(id), Label: screenLabel(id), Group: "Navigate", Keywords: "screen tab", Enabled: true, Screen: id})
	}
	actions = append(actions, Action{ID: "help", Label: "Show help", Group: "Help", Keywords: "commands keys", Shortcut: "?", Enabled: true, Run: func() tea.Cmd { return func() tea.Msg { return showHelpMsg{} } }}, Action{ID: "quit", Label: "Quit", Group: "Application", Enabled: true, Run: func() tea.Cmd { return tea.Quit }})
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
	header := m.header()
	footer := m.muted(components.Statusbar(statusActions(m.activeScreen())))
	if m.notification != "" {
		footer += "  " + m.notification
	}
	content := m.contentView(layout)
	if m.modal != modalNone {
		content = m.modalView(content, layout)
	}
	view := tea.NewView(strings.Join([]string{boundedLine(header, m.width), strings.Repeat("─", max(1, m.width)), content, strings.Repeat("─", max(1, m.width)), boundedLine(footer, m.width)}, "\n"))
	view.AltScreen = true
	return view
}

func (m model) header() string {
	profileName, machine := "default", "portable default"
	if m.session != nil {
		profileName = m.session.Profile().Manifest.Profile.Name
		if selection := m.session.Machine(); selection.Name != "" {
			machine = selection.Name + " (" + selection.Source + ")"
		}
	}
	state := "✓ overview clean"
	if overview, ok := m.screens[ScreenOverview].(headerStateScreen); ok {
		state = overview.HeaderState()
	}
	if strings.HasPrefix(state, "!") {
		state = m.warning(state)
	} else if strings.HasPrefix(state, "x") {
		state = m.color(state, m.palette.Error)
	} else if strings.HasPrefix(state, "~") {
		state = m.muted(state)
	}
	return m.accent("Blueprint") + "  profile: " + profileName + "  machine: " + machine + "  " + state
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
		if m.sidebarOpen {
			return m.sidebarScroll.render(strings.Split(components.Sidebar(items, m.selected, layout.workspaceWidth), "\n"), layout.workspaceWidth, layout.contentHeight)
		}
		return boundedBox(workspace, layout.workspaceWidth, layout.contentHeight)
	}
	sidebar := m.sidebarScroll.render(strings.Split(components.Sidebar(items, m.selected, layout.sidebarWidth), "\n"), layout.sidebarWidth, layout.contentHeight)
	workspace = boundedBox(workspace, layout.workspaceWidth, layout.contentHeight)
	if layout.mode == LayoutTwoPane {
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, "│", workspace)
	}
	details := "Details / preview"
	if detailed, ok := m.activeScreen().(DetailView); ok {
		details = detailed.DetailView()
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, "│", workspace, "│", boundedBox(details, layout.detailsWidth, layout.contentHeight))
}

func boundedBox(content string, width, height int) string {
	return lipgloss.NewStyle().Width(max(0, width)).Height(max(0, height)).MaxWidth(max(0, width)).MaxHeight(max(0, height)).Render(content)
}

func boundedLine(content string, width int) string {
	return lipgloss.NewStyle().Width(max(0, width)).MaxWidth(max(0, width)).Render(content)
}

func (m model) modalView(base string, layout layout) string {
	width, height := max(30, min(layout.workspaceWidth, m.width-8)), max(4, layout.contentHeight-2)
	var overlay string
	switch m.modal {
	case modalPalette:
		items := filterActions(m.actions(), m.paletteQuery)
		lines := strings.Split(components.Palette(m.paletteQuery, paletteItems(items), m.paletteSelected), "\n")
		overlay = m.paletteScroll.render(lines, width, height)
	case modalHelp:
		overlay = m.helpScroll.render(m.helpLines(), width, height)
	}
	return lipgloss.Place(m.width, layout.contentHeight, lipgloss.Center, lipgloss.Center, overlay, lipgloss.WithWhitespaceChars(" ")) + "\n" + base
}

func (m model) helpLines() []string {
	lines := strings.Split(components.Help(screenLabel(m.screenID()), statusActions(m.activeScreen())), "\n")
	focus := []string{"sidebar", "workspace", "details"}[m.focus]
	return append(lines, "Focus: "+focus)
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
		items = append(items, components.PaletteItem{ID: action.ID, Label: action.Label, Group: action.Group, Shortcut: action.Shortcut, Enabled: action.Enabled, DisabledReason: action.DisabledReason})
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
		{ID: "resources.discover", Label: "Discover resource", Group: "Resources", Shortcut: "n", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'n'}) }},
		{ID: "resources.untrack", Label: "Untrack resource", Group: "Resources", Shortcut: "x", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'x'}) }},
	}
}

func (s *machinesScreen) ID() ScreenID { return ScreenMachines }
func (s *machinesScreen) Actions() []Action {
	return []Action{
		{ID: "machines.add", Label: "Add machine", Group: "Machines", Shortcut: "a", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'a'}) }},
		{ID: "machines.map", Label: "Map resource directory", Group: "Machines", Shortcut: "m", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'm'}) }},
	}
}

func (s *restoreScreen) ID() ScreenID { return ScreenRestore }
func (s *restoreScreen) Actions() []Action {
	return []Action{{ID: "restore.force", Label: "Toggle forced restore", Group: "Restore", Shortcut: "f", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'f'}) }}, {ID: "restore.apply", Label: "Apply restore", Group: "Restore", Shortcut: "enter", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) }}}
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
		{ID: "config.diff", Label: "View Config diff", Group: "Config", Shortcut: "d", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'd'}) }},
		{ID: "config.include", Label: "Include Config path", Group: "Config", Shortcut: "i", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'i'}) }},
		{ID: "config.exclude", Label: "Exclude Config path", Group: "Config", Shortcut: "x", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'x'}) }},
		{ID: "config.auto", Label: "Use automatic Config policy", Group: "Config", Shortcut: "a", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'a'}) }},
		{ID: "config.edit", Label: "Edit Config file", Group: "Config", Shortcut: "e", Enabled: s.CanHandoff(), DisabledReason: "selected Config path is not a regular live file", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'e'}) }},
		{ID: "config.open", Label: "Open Config location", Group: "Config", Shortcut: "o", Enabled: s.CanHandoff(), DisabledReason: "selected Config path is not a regular live file", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'o'}) }},
		{ID: "config.copy", Label: "Copy Config path", Group: "Config", Shortcut: "y", Enabled: s.CanHandoff(), DisabledReason: "selected Config path is not a regular live file", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'y'}) }},
	}
}

func (s *overviewScreen) ID() ScreenID { return ScreenOverview }
func (s *overviewScreen) Actions() []Action {
	return []Action{{ID: "overview.refresh", Label: "Refresh Overview", Shortcut: "r", Enabled: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'r'}) }}}
}
