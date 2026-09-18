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
	sidebarOpen           bool
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
		m.welcomeBusy = false
		if created.err != nil {
			m.welcomeError = created.err
			return m, nil
		}
		dir := created.dir
		if dir == "" {
			dir = m.profileDir
		}
		fresh := newModelWithContext(m.ctx, m.cancel, m.themeLoader, created.session, dir, m.createProfile)
		fresh.width, fresh.height = m.width, m.height
		fresh.notification = "Profile " + created.verb + " at " + dir
		fresh.setScreenSizes()
		return fresh, fresh.Init()
	}
	if wrapped, ok := msg.(screenMsg); ok {
		return m.updateScreenMsg(wrapped)
	}
	if next, cmd, handled := m.handleRootMessage("", msg); handled {
		return next, cmd
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		layout := layoutForSize(m.width, m.height)
		if layout.mode == LayoutCompact {
			m.focus, m.sidebarOpen = focusWorkspace, false
		}
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
		transient := m.activeTransient()
		// Active transients always own their input. Outside a transient, only the
		// workspace may offer a key to its screen before root fallback.
		if transient || m.focus == focusWorkspace {
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
			if layoutForSize(m.width, m.height).mode == LayoutCompact {
				m.sidebarOpen = !m.sidebarOpen
				if m.sidebarOpen {
					m.focus = focusSidebar
				} else {
					m.focus = focusWorkspace
				}
				return m, nil
			}
			m.cycleFocus(isReverseTab(key))
			return m, nil
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
			if m.focus == focusDetails && layoutForSize(m.width, m.height).mode == LayoutThreePane {
				m.detailScroll.move(1, len(m.detailLines(layoutForSize(m.width, m.height))), layoutForSize(m.width, m.height).contentHeight-2)
				return m, nil
			}
		case "k", "up":
			if m.focus == focusSidebar {
				return m, m.moveScreen(-1)
			}
			if m.focus == focusDetails && layoutForSize(m.width, m.height).mode == LayoutThreePane {
				m.detailScroll.move(-1, len(m.detailLines(layoutForSize(m.width, m.height))), layoutForSize(m.width, m.height).contentHeight-2)
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

func (m model) activeTransient() bool {
	owner, ok := m.activeScreen().(transientScreen)
	return ok && owner.TransientActive()
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
	bindings := []Binding{{ActionID: "help", Label: "Show help", Key: "?", Context: "Global"}, {Label: "Commands", Key: ":", Context: "Global"}}
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
