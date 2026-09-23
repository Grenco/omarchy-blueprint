package tui

import (
	"context"
	"path/filepath"
	"sort"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type ScreenID string

const (
	ScreenOverview  ScreenID = "overview"
	ScreenCapture   ScreenID = "capture"
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

var screenOrder = orderedScreenIDs()

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
	modalStack            []modalKind
	requestedModal        *ModalRequest
	requestedModalScreen  ScreenID
	requestedModalInput   components.TextInputModal
	paletteOpen, helpOpen bool // Kept for package-local compatibility; modal is canonical.
	paletteQuery          string
	paletteFiltering      bool
	paletteSelected       int
	paletteInput          components.TextInputModal
	helpQuery             string
	helpInput             components.TextInputModal
	sidebarScroll         verticalViewport
	paletteScroll         verticalViewport
	helpScroll            verticalViewport
	detailScroll          verticalViewport
	notification          string
	screens               map[ScreenID]screen
	initialized           map[ScreenID]bool
	session               *workflow.Session
	ctx                   context.Context
	cancel                context.CancelFunc
	profileDir            string
	createProfile         func(context.Context, string, string) (*workflow.Session, error)
	openSession           func(workflow.Options) (*workflow.Session, error)
	machine               string
	welcomeChooser        bool
	welcomeStep           string
	welcomeChoice         int
	welcomePath           components.TextInputModal
	welcomeName           components.TextInputModal
	welcomeError          error
	welcomeBusy           bool
}

func newModel(loader ThemeLoader) model {
	return newModelWithContext(context.Background(), func() {}, loader, nil, "", nil)
}

func newModelWithSession(loader ThemeLoader, session *workflow.Session) model {
	return newModelWithContext(context.Background(), func() {}, loader, session, "", nil)
}

func newModelWithContext(ctx context.Context, cancel context.CancelFunc, loader ThemeLoader, session *workflow.Session, profileDir string, createProfile func(context.Context, string, string) (*workflow.Session, error)) model {
	if abs, err := filepath.Abs(profileDir); err == nil {
		profileDir = abs
	}
	screenMap := newScreens(ctx, session)
	palette := loader.Load()
	for _, current := range screenMap {
		if styled, ok := current.(styleableScreen); ok {
			styled.SetStyles(components.NewStyles(palette))
		}
	}
	m := model{palette: palette, themeLoader: loader, fingerprint: loader.Fingerprint(), screens: screenMap, initialized: map[ScreenID]bool{}, session: session, ctx: ctx, cancel: cancel, profileDir: profileDir, createProfile: createProfile}
	m.setScreenSizes()
	if session == nil && createProfile != nil {
		m.welcomeStep = "create-name"
		m.welcomeName = components.NewTextInputModal(filepath.Base(profileDir), "Profile name")
		m.openModal(modalWelcome)
	}
	return m
}

func (m model) Init() tea.Cmd {
	commands := []tea.Cmd{themeTickCmd()}
	if m.modal == modalWelcome {
		commands = append(commands, m.welcomeName.Focus())
	}
	commands = append(commands, m.initScreen(m.screenID()))
	return tea.Batch(commands...)
}

func wrapScreenCmd(id ScreenID, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			wrapped := make([]tea.Cmd, 0, len(batch))
			for _, batchCmd := range batch {
				wrapped = append(wrapped, wrapScreenCmd(id, batchCmd))
			}
			return tea.BatchMsg(wrapped)
		}
		return screenMsg{Screen: id, Msg: msg}
	}
}

func (m model) updateScreenMsg(wrapped screenMsg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.handleRootMessage(wrapped.Screen, wrapped.Msg); handled {
		return next, cmd
	}
	if current := m.screens[wrapped.Screen]; current != nil {
		return m, wrapScreenCmd(wrapped.Screen, current.Update(wrapped.Msg))
	}
	return m, nil
}

// newInspectPathCmd binds browser inspection to the workflow session without
// making the reusable component depend on session construction.
func newInspectPathCmd(ctx context.Context, session *workflow.Session) func(uint64, string) tea.Cmd {
	return func(requestID uint64, path string) tea.Cmd {
		return func() tea.Msg {
			inspection, err := session.InspectPath(ctx, path)
			return pathInspectedMsg{RequestID: requestID, Inspection: inspection, Err: err}
		}
	}
}

func (m model) activeScreen() screen { return m.screens[m.screenID()] }
func (m model) screenID() ScreenID   { return screenOrder[m.selected] }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if created, ok := msg.(profileCreatedMsg); ok {
		return m.handleProfileCreated(created)
	}
	if wrapped, ok := msg.(screenMsg); ok {
		return m.updateScreenMsg(wrapped)
	}
	if next, cmd, handled := m.handleRootMessage("", msg); handled {
		return next, cmd
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		before := layoutForSize(m.width, m.height).mode
		m.width, m.height = size.Width, size.Height
		if after := layoutForSize(m.width, m.height).mode; after != before && after == LayoutCompact {
			// Entering compact must not surface a drawer the user did not open.
			m.focus = focusWorkspace
		}
		m.normalizeFocus()
		m.setScreenSizes()
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
	if _, ok := msg.(clipboardCopiedMsg); ok {
		m.notification = "Copy requested."
		return m, nil
	}

	key, isKey := keyName(msg)
	if isKey && key == "ctrl+c" {
		m.stop()
		return m, tea.Quit
	}
	if m.modal == modalWelcome {
		return m.updateWelcome(msg, key, isKey)
	}
	if m.modal == modalPalette {
		return m.updatePalette(msg, key, isKey)
	}
	if m.modal == modalHelp && m.requestedModal == nil {
		return m.updateHelp(msg, key, isKey)
	}
	if m.modal == modalHelp || m.modal == modalConfirm {
		if updated, cmd, handled := m.updateRequestedModal(msg, key, isKey); handled {
			return updated, cmd
		}
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
	if isKey {
		if key == "tab" || key == "shift+tab" {
			if owner, ok := m.activeScreen().(TabOwner); ok && owner.OwnsTab() {
				// Local tabs belong to the workspace. While Navigation or Details
				// has focus, Tab must not change the screen behind it, and on a
				// tabbed screen it has no pane-cycling meaning either.
				if m.focus != focusWorkspace {
					return m, nil
				}
				if result := m.activeKeyResult(msg.(tea.KeyPressMsg)); result.Consumed {
					return m, wrapScreenCmd(m.screenID(), result.Cmd)
				}
			}
		}
		// Only a focused workspace offers keys to its screen before root
		// fallback. A transient screen owns every key while it has focus, but a
		// screen that turns transient on its own (e.g. while loading) must not
		// swallow keys meant for the focused Navigation or Details pane.
		if m.focus == focusWorkspace {
			if result := m.activeKeyResult(msg.(tea.KeyPressMsg)); result.Consumed {
				return m, wrapScreenCmd(m.screenID(), result.Cmd)
			}
		}
		if key == "q" {
			m.stop()
			return m, tea.Quit
		}
		switch key {
		case ":":
			m.openModal(modalPalette)
			m.paletteQuery, m.paletteSelected, m.paletteFiltering = "", 0, true
			m.paletteInput = components.NewTextInputModal("", "")
			return m, m.paletteInput.Focus()
		case "?":
			m.openModal(modalHelp)
			m.helpQuery = ""
			m.helpInput = components.NewTextInputModal("", "")
			return m, m.helpInput.Focus()
		case "tab", "shift+tab":
			// Compact drawers are reached with h/l only; Tab there belongs to
			// local tabs or does nothing.
			if layoutForSize(m.width, m.height).mode != LayoutCompact {
				m.cycleFocus(isReverseTab(key))
			}
			return m, nil
		case "esc":
			if m.drawerOpen() {
				m.focus = focusWorkspace
				return m, nil
			}
		case "[", "]":
			if m.focus == focusSidebar {
				destination := moveSidebarSection(m.screenID(), map[string]int{"[": -1, "]": 1}[key])
				if destination == m.screenID() {
					return m, nil
				}
				for index, id := range screenOrder {
					if id == destination {
						m.selected = index
						m.ensureSidebarSelection()
						return m, m.initScreen(destination)
					}
				}
			}
		case "j", "down":
			if m.focus == focusSidebar {
				return m, m.moveScreen(1)
			}
			if m.focus == focusDetails {
				m.scrollDetails(1)
				return m, nil
			}
		case "k", "up":
			if m.focus == focusSidebar {
				return m, m.moveScreen(-1)
			}
			if m.focus == focusDetails {
				m.scrollDetails(-1)
				return m, nil
			}
		case "h", "left":
			m.moveFocus(-1)
			return m, nil
		case "l", "right":
			m.moveFocus(1)
			return m, nil
		case "enter":
			// j/k already moved m.selected to the highlighted screen while the
			// Navigation drawer was open; Enter accepts it.
			if m.drawerOpen() && m.focus == focusSidebar {
				m.focus = focusWorkspace
				return m, nil
			}
		}
	}
	if !isKey {
		return m, m.updateActiveScreen(msg)
	}
	return m, nil
}

func (m model) stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m model) activeKeyResult(key tea.KeyPressMsg) KeyResult {
	handler, ok := m.activeScreen().(KeyHandler)
	if !ok {
		return KeyResult{}
	}
	return handler.HandleKey(key)
}

func (m model) updateActiveScreen(msg tea.Msg) tea.Cmd {
	if current := m.activeScreen(); current != nil {
		return wrapScreenCmd(m.screenID(), current.Update(msg))
	}
	return nil
}

func (m *model) moveScreen(delta int) tea.Cmd {
	m.selected = (m.selected + delta + len(screenOrder)) % len(screenOrder)
	m.ensureSidebarSelection()
	return m.initScreen(m.screenID())
}
func (m *model) selectScreen(id ScreenID) tea.Cmd {
	for i, candidate := range screenOrder {
		if candidate == id {
			m.selected, m.focus = i, focusWorkspace
			m.ensureSidebarSelection()
			return m.initScreen(id)
		}
	}
	return nil
}
func (m *model) initScreen(id ScreenID) tea.Cmd {
	if m.initialized == nil {
		m.initialized = map[ScreenID]bool{}
	}
	if m.initialized[id] {
		return nil
	}
	initializable, ok := m.screens[id].(initializableScreen)
	if !ok {
		return nil
	}
	m.initialized[id] = true
	return wrapScreenCmd(id, initializable.Init())
}

// panes lists, left to right, the focus areas the current layout can render
// for the active screen. It is the single source for horizontal movement in
// every layout: compact mode shows sidebar and details as drawers over the
// workspace rather than side by side, but the order is the same.
func (m model) panes() []focusArea {
	panes := []focusArea{focusSidebar, focusWorkspace}
	if m.hasDetailsPane() {
		panes = append(panes, focusDetails)
	}
	return panes
}

func (m model) hasDetailsPane() bool {
	switch layoutForSize(m.width, m.height).mode {
	case LayoutThreePane, LayoutCompact:
		_, ok := m.activeScreen().(DetailView)
		return ok
	}
	return false
}

// drawerOpen reports whether a compact drawer is showing. In compact mode the
// drawer is derived from focus, so focus can never name a hidden pane.
func (m model) drawerOpen() bool {
	return layoutForSize(m.width, m.height).mode == LayoutCompact && m.focus != focusWorkspace
}

// moveFocus steps along panes without wrapping, so h/l stop at the edges.
func (m *model) moveFocus(delta int) {
	panes := m.panes()
	for i, pane := range panes {
		if pane == m.focus {
			m.focus = panes[max(0, min(len(panes)-1, i+delta))]
			return
		}
	}
	m.focus = focusWorkspace
}

func (m *model) cycleFocus(reverse bool) {
	panes := m.panes()
	delta := 1
	if reverse {
		delta = -1
	}
	for i, pane := range panes {
		if pane == m.focus {
			m.focus = panes[(i+delta+len(panes))%len(panes)]
			return
		}
	}
	m.focus = focusWorkspace
}

// normalizeFocus returns focus to the workspace whenever it names a pane the
// current layout or screen cannot render.
func (m *model) normalizeFocus() {
	for _, pane := range m.panes() {
		if pane == m.focus {
			return
		}
	}
	m.focus = focusWorkspace
}

func (m *model) scrollDetails(delta int) {
	lines := m.detailLines(m.detailsInnerWidth())
	m.detailScroll.move(delta, len(lines), layoutForSize(m.width, m.height).contentHeight-2)
}

func (m model) actions() []Action {
	actions := make([]Action, 0, len(screenOrder)+4)
	if current := m.activeScreen(); current != nil {
		for _, action := range current.Actions() {
			if !action.Visible {
				continue
			}
			action.Screen = m.screenID()
			if action.Group == "" {
				action.Group = "Actions"
			}
			action.PaletteInitial = true
			actions = append(actions, action)
		}
	}
	for _, id := range screenOrder {
		info := screenInfo(id)
		actions = append(actions, Action{ID: "screen." + string(id), Label: info.Label, Group: "Navigate", Keywords: info.Keywords, Enabled: true, Visible: true, Screen: id})
	}
	actions = append(actions, Action{ID: "help", Label: "Show help", Group: "Help", Keywords: "commands keys", Enabled: true, Visible: true, PaletteInitial: true, Run: func() tea.Cmd { return func() tea.Msg { return showHelpMsg{} } }}, Action{ID: "quit", Label: "Quit", Group: "Application", Enabled: true, Visible: true, PaletteInitial: true, Run: func() tea.Cmd { return tea.Quit }})
	return actions
}

func (m model) paletteActions() []Action {
	registry := ActionRegistry{Actions: m.actions(), Bindings: m.bindings()}
	if m.paletteQuery == "" {
		return registry.Initial()
	}
	return registry.Search(m.paletteQuery)
}

func (m model) bindings() []Binding {
	// The status bar always leads with "? help   : commands", so the global
	// bindings stay in help and the palette but not the footer.
	bindings := []Binding{{ActionID: "help", Label: "Show help", Key: "?", Context: "Global", HideFromFooter: true}, {Label: "Commands", Key: ":", Context: "Global", HideFromFooter: true}}
	bindings = append(bindings, m.paneBindings()...)
	if m.focus == focusSidebar {
		return append(bindings,
			Binding{Label: "Previous sidebar section", Key: "[", Context: "Sidebar", HideFromFooter: true},
			Binding{Label: "Next sidebar section", Key: "]", Context: "Sidebar", HideFromFooter: true},
		)
	}
	if current, ok := m.activeScreen().(bindingScreen); ok {
		for _, binding := range current.Bindings() {
			if m.focus != focusWorkspace && binding.HideFromFooter {
				continue
			}
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

// paneBindings advertises how to move between panes: in compact mode the
// drawers are otherwise invisible until opened, and a focused Details pane in
// any layout only accepts scrolling and leaving.
func (m model) paneBindings() []Binding {
	if layoutForSize(m.width, m.height).mode != LayoutCompact {
		if m.focus == focusDetails {
			return []Binding{{Label: "Scroll", Key: "j/k", Context: "Details", FooterPriority: -5}, {Label: "Back to workspace", Key: "h", Context: "Details", FooterPriority: -5}}
		}
		return nil
	}
	switch m.focus {
	case focusSidebar:
		return []Binding{{Label: "Open screen", Keys: []string{"enter", "l"}, Context: "Navigation", FooterPriority: -5}, {Label: "Close", Key: "esc", Context: "Navigation", FooterPriority: -5}}
	case focusDetails:
		return []Binding{{Label: "Scroll", Key: "j/k", Context: "Details", FooterPriority: -5}, {Label: "Close", Keys: []string{"esc", "h"}, Context: "Details", FooterPriority: -5}}
	}
	bindings := []Binding{{Label: "Navigation", Key: "h", Context: "Layout", FooterPriority: -5}}
	if m.hasDetailsPane() {
		bindings = append(bindings, Binding{Label: "Details", Key: "l", Context: "Layout", FooterPriority: -5})
	}
	return bindings
}

func statusActions(current screen, bindings []Binding) []components.StatusAction {
	actions := []components.StatusAction{}
	if current != nil {
		available := map[string]Action{}
		for _, action := range visibleActions(current.Actions()) {
			available[action.ID] = action
		}
		sort.SliceStable(bindings, func(i, j int) bool { return bindings[i].FooterPriority < bindings[j].FooterPriority })
		for _, binding := range bindings {
			if binding.HideFromFooter {
				continue
			}
			key := binding.DisplayKeys()
			if binding.ActionID == "" {
				actions = append(actions, components.StatusAction{Label: binding.Label, Shortcut: key, Enabled: true})
				continue
			}
			if action, ok := available[binding.ActionID]; ok {
				actions = append(actions, components.StatusAction{Label: action.Label, Shortcut: key, Enabled: action.Enabled})
			} else if binding.Label != "" {
				actions = append(actions, components.StatusAction{Label: binding.Label, Shortcut: key, Enabled: true})
			}
		}
	}
	return actions
}

func paletteItems(actions []Action, bindings []Binding) []components.PaletteItem {
	keys := map[string]string{}
	for _, binding := range bindings {
		keys[binding.ActionID] = binding.DisplayKeys()
	}
	items := make([]components.PaletteItem, 0, len(actions))
	for _, action := range actions {
		items = append(items, components.PaletteItem{ID: action.ID, Label: action.Label, Group: action.Group, Shortcut: keys[action.ID], Enabled: action.Enabled, DisabledReason: action.DisabledReason})
	}
	return items
}

func screenLabel(id ScreenID) string {
	return screenInfo(id).Label
}
