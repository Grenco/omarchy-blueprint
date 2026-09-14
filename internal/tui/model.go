package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
type headerStateScreen interface{ HeaderState() string }
type bindingScreen interface{ Bindings() []Binding }

// DetailView is optional: screens that provide it own the third pane preview.
type DetailView interface{ DetailView() string }

// ModalRequest lets any screen ask the root to render transient content over
// the unchanged workspace. Screens retain ownership of the response message.
type ModalRequest struct {
	Title, Content, Input, Placeholder string
}

type modalKind uint8

const (
	modalNone modalKind = iota
	modalPalette
	modalHelp
	modalConfirm
	modalWelcome
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

type profileCreatedMsg struct {
	session *workflow.Session
	dir     string
	verb    string
	err     error
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
	screenMap := make(map[ScreenID]screen, len(screenOrder))
	for _, id := range screenOrder {
		screenMap[id] = &placeholderScreen{id: id}
	}
	if session != nil {
		screenMap[ScreenOverview] = &overviewScreen{Overview: screens.NewOverviewContext(ctx, session)}
		screenMap[ScreenCapture] = &captureScreen{Capture: screens.NewCaptureContext(ctx, session)}
		screenMap[ScreenConfig] = &configScreen{Config: screens.NewConfigContext(ctx, session)}
		screenMap[ScreenResources] = &resourcesScreen{Resources: screens.NewResourcesContext(ctx, session)}
		screenMap[ScreenMachines] = &machinesScreen{Machines: screens.NewMachinesContext(ctx, session)}
		screenMap[ScreenRestore] = &restoreScreen{Restore: screens.NewRestoreContext(ctx, session)}
		screenMap[ScreenSync] = &syncScreen{Sync: screens.NewSyncContext(ctx, session)}
		for _, id := range []ScreenID{ScreenPackages, ScreenThemes, ScreenPlugins, ScreenShell, ScreenHooks, ScreenDefaults} {
			screenMap[id] = &providerScreen{Provider: screens.NewProviderContext(ctx, session, string(id)), id: id}
		}
	}
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

func (m *model) enableProfileChooser(openSession func(workflow.Options) (*workflow.Session, error), machine string) {
	m.openSession, m.machine, m.welcomeChooser, m.welcomeStep, m.welcomeChoice = openSession, machine, true, "choose", 0
	path := filepath.Join(filepath.Dir(m.profileDir), "omarchy-profile")
	if home, err := os.UserHomeDir(); err == nil {
		path = filepath.Join(home, "omarchy-profile")
	}
	m.welcomePath = components.NewTextInputModal(path, "Profile path")
	m.openModal(modalWelcome)
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
	// Root-owned requests are still identified by their originating screen.
	switch msg := wrapped.Msg.(type) {
	case showHelpMsg:
		m.openModal(modalHelp)
		return m, nil
	case components.ModalRequest:
		m.requestedModal = &ModalRequest{Title: msg.Title, Content: msg.Content, Input: msg.Input, Placeholder: msg.Placeholder}
		m.requestedModalScreen = wrapped.Screen
		m.openModal(modalConfirm)
		if msg.Input != "" || msg.Placeholder != "" {
			m.requestedModalInput = components.NewTextInputModal(msg.Input, msg.Placeholder)
			return m, m.requestedModalInput.Focus()
		}
		return m, nil
	case screens.OverviewTarget:
		navigation := m.selectScreen(ScreenID(msg.Target))
		switch current := m.activeScreen().(type) {
		case *configScreen:
			return m, tea.Batch(navigation, wrapScreenCmd(m.screenID(), current.Focus(msg.Ref)))
		case *resourcesScreen:
			return m, tea.Batch(navigation, wrapScreenCmd(m.screenID(), current.Focus(msg.Ref)))
		case *machinesScreen:
			current.Focus(msg.Ref)
		}
		return m, navigation
	case screens.CaptureComplete:
		return m.handleCaptureComplete(msg)
	case screens.SessionReloadNeeded:
		return m.reloadSessionScreens(msg.Reason)
	case screens.HandoffRequest:
		return m.handleHandoffRequest(msg)
	case handoffFinishedMsg:
		if msg.Kind == "lazygit" {
			return m.reloadSessionScreens("LazyGit")
		}
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
	case screens.Notice:
		m.notification = msg.Message
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
	if _, ok := msg.(showHelpMsg); ok {
		m.openModal(modalHelp)
		return m, nil
	}
	if target, ok := msg.(screens.OverviewTarget); ok {
		navigation := m.selectScreen(ScreenID(target.Target))
		switch current := m.activeScreen().(type) {
		case *configScreen:
			return m, tea.Batch(navigation, wrapScreenCmd(m.screenID(), current.Focus(target.Ref)))
		case *resourcesScreen:
			return m, tea.Batch(navigation, wrapScreenCmd(m.screenID(), current.Focus(target.Ref)))
		case *machinesScreen:
			current.Focus(target.Ref)
		}
		return m, navigation
	}
	if complete, ok := msg.(screens.CaptureComplete); ok {
		return m.handleCaptureComplete(complete)
	}
	if notice, ok := msg.(screens.Notice); ok {
		m.notification = notice.Message
		return m, nil
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
	if finished, ok := msg.(handoffFinishedMsg); ok {
		if finished.Kind == "lazygit" {
			return m.reloadSessionScreens("LazyGit")
		}
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
		if m.requestedModal != nil && (m.requestedModal.Input != "" || m.requestedModal.Placeholder != "") && isKey {
			if key == "esc" {
				m.closeModal()
				_, cmd := m.updateScreenMsg(screenMsg{Screen: m.requestedModalScreen, Msg: tea.KeyPressMsg{Code: tea.KeyEsc}})
				return m, cmd
			}
			if key == "enter" {
				screenID, value := m.requestedModalScreen, m.requestedModalInput.Value()
				m.closeModal()
				_, cmd := m.updateScreenMsg(screenMsg{Screen: screenID, Msg: components.TextInputSubmitted{Value: value}})
				return m, cmd
			}
			return m, m.requestedModalInput.Update(msg)
		}
		if m.requestedModal != nil && isKey && (key == "enter" || key == "esc") {
			screenID := m.requestedModalScreen
			m.closeModal()
			_, cmd := m.updateScreenMsg(screenMsg{Screen: screenID, Msg: tea.KeyPressMsg{Code: map[bool]rune{true: tea.KeyEnter, false: tea.KeyEsc}[key == "enter"]}})
			return m, cmd
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

func (m model) updateWelcome(msg tea.Msg, key string, isKey bool) (tea.Model, tea.Cmd) {
	if m.welcomeChooser && m.welcomeStep == "choose" {
		if isKey {
			switch key {
			case "q", "esc":
				m.stop()
				return m, tea.Quit
			case "up", "k":
				m.welcomeChoice = max(0, m.welcomeChoice-1)
			case "down", "j":
				m.welcomeChoice = min(2, m.welcomeChoice+1)
			case "enter":
				switch m.welcomeChoice {
				case 0:
					m.welcomeStep = "create-path"
					return m, m.welcomePath.Focus()
				case 1:
					m.welcomeStep = "open-path"
					return m, m.welcomePath.Focus()
				default:
					m.stop()
					return m, tea.Quit
				}
			}
		}
		return m, nil
	}
	if isKey {
		switch key {
		case "q":
			m.stop()
			return m, tea.Quit
		case "esc":
			if m.welcomeChooser {
				m.welcomeStep, m.welcomeError = "choose", nil
				return m, nil
			}
			m.stop()
			return m, tea.Quit
		case "enter":
			if m.welcomeStep == "create-path" {
				path := expandWelcomePath(strings.TrimSpace(m.welcomePath.Value()))
				if path == "" {
					m.welcomeError = fmt.Errorf("profile path is required")
					return m, nil
				}
				m.profileDir, m.welcomeStep = path, "create-name"
				m.welcomeName = components.NewTextInputModal(filepath.Base(filepath.Clean(path)), "Profile name")
				return m, m.welcomeName.Focus()
			}
			if m.welcomeStep == "open-path" {
				path := expandWelcomePath(strings.TrimSpace(m.welcomePath.Value()))
				if path == "" {
					m.welcomeError = fmt.Errorf("profile path is required")
					return m, nil
				}
				if !m.welcomeBusy {
					m.welcomeBusy, m.welcomeError = true, nil
					return m, func() tea.Msg {
						session, err := m.openSession(workflow.Options{ProfileDir: path, ExplicitMachine: m.machine})
						dir := path
						if session != nil {
							dir = session.ProfileDir()
						}
						return profileCreatedMsg{session: session, dir: dir, verb: "opened", err: err}
					}
				}
				return m, nil
			}
			name := strings.TrimSpace(m.welcomeName.Value())
			if name == "" {
				m.welcomeError = fmt.Errorf("profile name is required")
				return m, nil
			}
			if !m.welcomeBusy {
				m.welcomeBusy, m.welcomeError = true, nil
				return m, func() tea.Msg {
					session, err := m.createProfile(m.ctx, m.profileDir, name)
					dir := m.profileDir
					if session != nil {
						dir = session.ProfileDir()
					}
					return profileCreatedMsg{session: session, dir: dir, verb: "created", err: err}
				}
			}
		}
	}
	var cmd tea.Cmd
	if m.welcomeStep == "create-path" || m.welcomeStep == "open-path" {
		cmd = m.welcomePath.Update(msg)
	} else {
		cmd = m.welcomeName.Update(msg)
	}
	return m, cmd
}

func expandWelcomePath(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
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

func (m model) handleCaptureComplete(complete screens.CaptureComplete) (tea.Model, tea.Cmd) {
	if complete.Err != nil {
		m.notification = "Capture failed: " + complete.Err.Error()
		return m, nil
	}
	m.notification = "Capture complete. Overview and Profile Git status refreshed."
	if complete.Warning != nil {
		m.notification = "Capture applied; cleanup warning: " + complete.Warning.Error()
	}
	commands := []tea.Cmd{}
	if capture, ok := m.screens[ScreenCapture].(*captureScreen); ok {
		commands = append(commands, wrapScreenCmd(ScreenCapture, capture.Init()))
	}
	if overview, ok := m.screens[ScreenOverview].(*overviewScreen); ok {
		commands = append(commands, wrapScreenCmd(ScreenOverview, overview.Update(tea.KeyPressMsg{Code: 'r'})))
	}
	if sync, ok := m.screens[ScreenSync].(*syncScreen); ok {
		commands = append(commands, wrapScreenCmd(ScreenSync, sync.Update(tea.KeyPressMsg{Code: 'r'})))
	}
	if provider, ok := m.screens[ScreenID(complete.Provider)].(*providerScreen); ok {
		commands = append(commands, wrapScreenCmd(ScreenID(complete.Provider), provider.Refresh()))
	}
	for _, id := range complete.Providers {
		if provider, ok := m.screens[ScreenID(id)].(*providerScreen); ok {
			commands = append(commands, wrapScreenCmd(ScreenID(id), provider.Refresh()))
		}
	}
	return m, tea.Batch(commands...)
}

func (m model) reloadSessionScreens(reason string) (tea.Model, tea.Cmd) {
	if m.session == nil {
		return m, nil
	}
	if err := m.session.Reload(); err != nil {
		m.notification = "Profile reload failed after " + reason + ": " + err.Error()
		return m, nil
	}
	m.notification = "Profile reloaded after " + reason + "."
	commands := make([]tea.Cmd, 0, len(m.screens))
	for id, current := range m.screens {
		if !m.initialized[id] {
			continue
		}
		if initializable, ok := current.(initializableScreen); ok {
			commands = append(commands, wrapScreenCmd(id, initializable.Init()))
		}
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
	if m.modal != modalNone {
		m.modalStack = append(m.modalStack, m.modal)
	}
	m.modal = kind
	m.paletteOpen, m.helpOpen = kind == modalPalette, kind == modalHelp
}

func (m *model) closeModal() {
	if len(m.modalStack) > 0 {
		m.modal = m.modalStack[len(m.modalStack)-1]
		m.modalStack = m.modalStack[:len(m.modalStack)-1]
	} else {
		m.modal = modalNone
	}
	m.paletteOpen, m.helpOpen = m.modal == modalPalette, m.modal == modalHelp
	m.requestedModal = nil
	m.requestedModalScreen = ""
	m.requestedModalInput = components.TextInputModal{}
}

func (m *model) updatePalette(msg tea.Msg, key string, isKey bool) (tea.Model, tea.Cmd) {
	items := m.paletteActions()
	if isKey {
		switch key {
		case "esc":
			m.closeModal()
			return *m, nil
		case "up":
			if m.paletteSelected > 0 {
				m.paletteSelected--
			}
			m.paletteScroll.ensure(m.paletteSelected, len(items), layoutForSize(m.width, m.height).contentHeight-2)
			return *m, nil
		case "down":
			if m.paletteSelected < len(items)-1 {
				m.paletteSelected++
			}
			m.paletteScroll.ensure(m.paletteSelected, len(items), layoutForSize(m.width, m.height).contentHeight-2)
			return *m, nil
		case "enter":
			if len(items) > 0 && items[m.paletteSelected].Enabled {
				action := items[m.paletteSelected]
				m.closeModal()
				var navigation tea.Cmd
				if action.Screen != "" {
					navigation = m.selectScreen(action.Screen)
				}
				if action.Run != nil {
					if action.Screen == "" {
						return *m, tea.Batch(navigation, action.Run())
					}
					return *m, tea.Batch(navigation, wrapScreenCmd(action.Screen, action.Run()))
				}
				return *m, navigation
			}
			return *m, nil
		}
	}
	previousQuery := m.paletteQuery
	cmd := m.paletteInput.Update(msg)
	m.paletteQuery = m.paletteInput.Value()
	items = m.paletteActions()
	if m.paletteQuery != previousQuery {
		m.paletteSelected, m.paletteScroll.offset = 0, 0
	} else if m.paletteSelected >= len(items) {
		m.paletteSelected = max(0, len(items)-1)
	}
	m.paletteScroll.ensure(m.paletteSelected, len(items), layoutForSize(m.width, m.height).contentHeight-2)
	return *m, cmd
}

func (m *model) updateHelp(msg tea.Msg, key string, isKey bool) (tea.Model, tea.Cmd) {
	if isKey {
		switch key {
		case "esc":
			m.closeModal()
			return *m, nil
		case "up":
			m.helpScroll.move(-1, len(m.helpLines()), layoutForSize(m.width, m.height).contentHeight-2)
			return *m, nil
		case "down":
			m.helpScroll.move(1, len(m.helpLines()), layoutForSize(m.width, m.height).contentHeight-2)
			return *m, nil
		}
	}
	cmd := m.helpInput.Update(msg)
	m.helpQuery = m.helpInput.Value()
	m.helpScroll.offset = 0
	return *m, cmd
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
	footer := m.footer()
	if m.notification != "" {
		footer += "  " + components.DisplayText(m.notification)
	}
	content := m.contentView(layout)
	if m.modal != modalNone {
		content = m.modalView(content, layout)
	}
	view := tea.NewView(strings.Join([]string{boundedLine(header, m.width), strings.Repeat("─", max(1, m.width)), content, strings.Repeat("─", max(1, m.width)), boundedLine(footer, m.width)}, "\n"))
	view.AltScreen = true
	return view
}

func (m model) footer() string {
	if m.modal != modalNone {
		switch m.modal {
		case modalPalette:
			return "type search   up/down select   enter run   esc close"
		case modalHelp:
			if m.requestedModal == nil {
				return "type search   up/down scroll   esc close"
			}
			if m.requestedModal.Input != "" || m.requestedModal.Placeholder != "" {
				return "enter save   esc cancel"
			}
			return "enter confirm   esc cancel"
		case modalWelcome:
			if m.welcomeChooser && m.welcomeStep == "choose" {
				return "up/down select   enter continue   esc quit"
			}
			if m.welcomeChooser {
				return "enter continue   esc back"
			}
			return "enter create   esc quit"
		default:
			if m.requestedModal != nil {
				if m.requestedModal.Input != "" || m.requestedModal.Placeholder != "" {
					return "enter save   esc cancel"
				}
				return "enter confirm   esc cancel"
			}
			return "esc close"
		}
	}
	return components.Statusbar(statusActions(m.activeScreen(), m.bindings()))
}

func (m model) header() string {
	profileName, machine := "default", "portable default"
	if m.session != nil {
		profileName = components.DisplayText(m.session.Profile().Manifest.Profile.Name)
		if selection := m.session.Machine(); selection.Name != "" {
			machine = components.DisplayText(selection.Name) + " (" + components.DisplayText(selection.Source) + ")"
		}
	}
	state := components.Icons.Ready + " overview clean"
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
	workspace := m.workspaceContent(layout.workspaceWidth)
	styles := components.NewStyles(m.palette)
	if layout.mode == LayoutCompact {
		if m.sidebarOpen {
			sidebar := m.sidebarRender(layout.workspaceWidth - 2)
			return components.Panel("Navigation", m.focus == focusSidebar, layout.workspaceWidth, layout.contentHeight, m.sidebarScroll.render(sidebar.Lines, layout.workspaceWidth-2, layout.contentHeight-2), styles)
		}
		return components.Panel(screenLabel(m.screenID()), m.focus == focusWorkspace, layout.workspaceWidth, layout.contentHeight, workspace, styles)
	}
	sidebarRender := m.sidebarRender(layout.sidebarWidth - 2)
	sidebar := components.Panel("Navigation", m.focus == focusSidebar, layout.sidebarWidth, layout.contentHeight, m.sidebarScroll.render(sidebarRender.Lines, layout.sidebarWidth-2, layout.contentHeight-2), styles)
	workspace = components.Panel(screenLabel(m.screenID()), m.focus == focusWorkspace, layout.workspaceWidth, layout.contentHeight, workspace, styles)
	if layout.mode == LayoutTwoPane {
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, "│", workspace)
	}
	_, ok := m.activeScreen().(DetailView)
	if !ok {
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, "│", workspace)
	}
	innerWidth, innerHeight := components.InteriorSize(layout.detailsWidth, layout.contentHeight)
	details := components.Panel("Details", m.focus == focusDetails, layout.detailsWidth, layout.contentHeight, m.detailScroll.render(m.detailLines(layout), innerWidth, innerHeight), styles)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, "│", workspace, "│", details)
}

func (m model) workspaceContent(panelWidth int) string {
	width, _ := components.InteriorSize(panelWidth, 0)
	description := components.WrapText(screenInfo(m.screenID()).Short, width)
	for i, line := range description {
		description[i] = m.muted(line)
	}
	return strings.Join(description, "\n") + "\n\n" + m.activeScreen().View()
}

func (m model) screenSize(id ScreenID) (int, int) {
	layout := layoutForSize(m.width, m.height)
	width, height := components.InteriorSize(layout.workspaceWidth, layout.contentHeight)
	descriptionLines := len(components.WrapText(screenInfo(id).Short, width))
	return width, max(0, height-descriptionLines-1)
}

func (m *model) setScreenSizes() {
	for id, current := range m.screens {
		width, height := m.screenSize(id)
		current.SetSize(width, height)
	}
}

func (m model) sidebarRender(width int) components.SidebarRender {
	return components.RenderSidebar(sidebarSections(), string(m.screenID()), width, components.NewStyles(m.palette), m.focus == focusSidebar)
}

func (m *model) ensureSidebarSelection() {
	layout := layoutForSize(m.width, m.height)
	width := layout.sidebarWidth
	if layout.mode == LayoutCompact {
		width = layout.workspaceWidth
	}
	_, height := components.InteriorSize(width, layout.contentHeight)
	rendered := m.sidebarRender(width - 2)
	m.sidebarScroll.ensure(rendered.SelectedLine, len(rendered.Lines), height)
}

func (m model) detailLines(layout layout) []string {
	detailed, ok := m.activeScreen().(DetailView)
	if !ok {
		return nil
	}
	width, _ := components.InteriorSize(layout.detailsWidth, layout.contentHeight)
	return components.WrapText(detailed.DetailView(), width)
}

func boundedBox(content string, width, height int) string {
	return lipgloss.NewStyle().Width(max(0, width)).Height(max(0, height)).MaxWidth(max(0, width)).MaxHeight(max(0, height)).Render(content)
}

func boundedLine(content string, width int) string {
	return lipgloss.NewStyle().Width(max(0, width)).MaxWidth(max(0, width)).Render(content)
}

// composeOverlay replaces cells in the centered overlay rectangle rather than
// appending it below the base view. The dimmed base remains visible around it.
func composeOverlay(base, overlay string, width, height int, color bool) string {
	baseLines, overlayLines := strings.Split(base, "\n"), strings.Split(overlay, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, "")
	}
	overlayWidth, overlayHeight := 0, len(overlayLines)
	for _, line := range overlayLines {
		overlayWidth = max(overlayWidth, lipgloss.Width(line))
	}
	x, y := max(0, (width-overlayWidth)/2), max(0, (height-overlayHeight)/2)
	for row := 0; row < height; row++ {
		line := boundedLine(baseLines[row], width)
		if color {
			line = lipgloss.NewStyle().Faint(true).Render(line)
		}
		if row < y || row >= y+overlayHeight {
			baseLines[row] = line
			continue
		}
		// Panels use bounded ASCII/Unicode cells; retain the base margins and
		// replace the central rectangle without adding rows to the base.
		prefix := lipgloss.NewStyle().MaxWidth(x).Render(line)
		suffixWidth := max(0, width-x-lipgloss.Width(overlayLines[row-y]))
		suffix := strings.Repeat(" ", suffixWidth)
		baseLines[row] = prefix + overlayLines[row-y] + suffix
	}
	return strings.Join(baseLines[:height], "\n")
}

func (m model) modalView(base string, layout layout) string {
	width, height := max(30, min(layout.workspaceWidth, m.width-8)), max(4, layout.contentHeight-2)
	innerWidth, innerHeight := components.InteriorSize(width, height)
	var overlay string
	styles := components.NewStyles(m.palette)
	title := ""
	switch m.modal {
	case modalPalette:
		title = "Command palette"
		items := m.paletteActions()
		paletteContent := components.PaletteItems(paletteItems(items, m.bindings()), m.paletteSelected, styles)
		if len(items) == 0 {
			paletteContent = "No matching commands"
		}
		content := "Search: " + m.paletteInput.View() + "\n" + paletteContent
		lines := strings.Split(content, "\n")
		overlay = m.paletteScroll.render(lines, innerWidth, innerHeight)
	case modalHelp:
		title = "Help"
		lines := m.helpLines()
		if m.requestedModal != nil {
			title, lines = m.requestedModal.Title, strings.Split(m.requestedModal.Content, "\n")
			if m.requestedModal.Input != "" || m.requestedModal.Placeholder != "" {
				lines = append(lines, "", m.requestedModalInput.View())
			}
		}
		overlay = m.helpScroll.render(lines, innerWidth, innerHeight)
	case modalWelcome:
		title = "Welcome to Omarchy Blueprint"
		lines := []string{}
		if m.welcomeChooser && m.welcomeStep == "choose" {
			lines = append(lines, "No Blueprint profile was found.", "", "Choose what to do:")
			for i, choice := range []string{"Create a new profile", "Open an existing profile", "Quit"} {
				marker := "  "
				if i == m.welcomeChoice {
					marker = "> "
				}
				lines = append(lines, marker+choice)
			}
		} else if m.welcomeStep == "create-path" || m.welcomeStep == "open-path" {
			action := "Create profile at:"
			if m.welcomeStep == "open-path" {
				action = "Open profile at:"
			}
			lines = append(lines, action, "", m.welcomePath.View())
		} else {
			lines = append(lines, "Create a new profile at:", components.DisplayText(m.profileDir), "", "Profile name: "+m.welcomeName.View())
		}
		if m.welcomeBusy {
			lines = append(lines, "", "Working...")
		}
		if m.welcomeError != nil {
			lines = append(lines, "Error: "+components.DisplayText(m.welcomeError.Error()))
		}
		overlay = strings.Join(lines, "\n")
	case modalConfirm:
		if m.requestedModal != nil {
			title, overlay = m.requestedModal.Title, m.requestedModal.Content
			if m.requestedModal.Input != "" || m.requestedModal.Placeholder != "" {
				overlay += "\n\n" + m.requestedModalInput.View()
			}
		}
	}
	if m.modal == modalConfirm {
		width = max(40, min(width, 68))
		height = max(5, min(height, len(strings.Split(overlay, "\n"))+4))
	}
	overlay = components.Panel(title, true, width, height, overlay, styles)
	if m.modal == modalConfirm {
		return composeOverlay(base, overlay, m.width, layout.contentHeight, m.palette.ColorEnabled)
	}
	return composeOverlay(base, overlay, m.width, layout.contentHeight, m.palette.ColorEnabled)
}

func (m model) helpLines() []string {
	info := screenInfo(m.screenID())
	lines := []string{strings.ToUpper(info.Label), ""}
	lines = append(lines, components.WrapText(info.Long, max(1, layoutForSize(m.width, m.height).workspaceWidth-4))...)
	lines = append(lines, "", "Search: "+m.helpInput.View(), "")
	registry := ActionRegistry{Actions: m.actions(), Bindings: m.bindings()}
	entries := registry.SearchBindings(m.helpQuery)
	keyEntries, globalEntries := make([]HelpEntry, 0, len(entries)), make([]HelpEntry, 0, 3)
	for _, entry := range entries {
		if entry.Context == "Global" {
			globalEntries = append(globalEntries, entry)
		} else {
			keyEntries = append(keyEntries, entry)
		}
	}
	if m.helpQuery != "" {
		keyEntries = entries
		globalEntries = nil
	}
	lines = append(lines, "KEYS")
	lines = appendHelpEntries(lines, keyEntries)
	if len(keyEntries) == 0 {
		lines = append(lines, "No matching help.")
	}
	if m.helpQuery == "" {
		lines = append(lines, "", "GLOBAL")
		lines = appendHelpEntries(lines, globalEntries)
		lines = append(lines, "q              Quit")
	}
	focus := []string{"sidebar", "workspace", "details"}[m.focus]
	return append(lines, "", "Focus: "+focus)
}

func appendHelpEntries(lines []string, entries []HelpEntry) []string {
	for _, entry := range entries {
		context := entry.Context
		if context == "" {
			context = entry.Group
		}
		if context != "" {
			lines = append(lines, fmt.Sprintf("%-14s %s: %s", entry.Key, context, entry.Label))
		} else {
			lines = append(lines, fmt.Sprintf("%-14s %s", entry.Key, entry.Label))
		}
	}
	return lines
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

func (s *resourcesScreen) ID() ScreenID { return ScreenResources }
func (s *resourcesScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !s.TransientActive() && !screenKey(key.String()) && key.String() != "tab" {
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
		{ID: "machines.remove", Label: "Remove machine", Group: "Machines", Enabled: selectedMachine, Visible: true, DisabledReason: "select a machine", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'x'}) }},
		{ID: "machines.map", Label: "Map resource directory", Group: "Machines", Enabled: s.CanMapResource(), Visible: true, DisabledReason: "select a resource path", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'm'}) }},
	}
}
func (s *machinesScreen) Bindings() []Binding {
	return []Binding{{ActionID: "machines.add", Key: "a"}, {ActionID: "machines.primary", Key: "u"}, {ActionID: "machines.clear", Key: "c"}, {ActionID: "machines.rename", Key: "r"}, {ActionID: "machines.remove", Key: "x"}, {ActionID: "machines.map", Key: "m"}}
}

func (s *restoreScreen) ID() ScreenID { return ScreenRestore }
func (s *restoreScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !s.TransientActive() && !screenKey(key.String()) {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *restoreScreen) Actions() []Action {
	label := "Restore all (normal)"
	if s.Mode() == workflow.RestoreForced {
		label = "Restore all (forced)"
	}
	return []Action{{ID: "restore.force", Label: "Toggle forced restore", Group: "Restore", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'f'}) }}, {ID: "restore.detail", Label: "View restore detail", Group: "Restore", Enabled: s.CanDetail(), Visible: true, DisabledReason: "selected consequence has no diff", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'd'}) }}, {ID: "restore.apply", Label: label, Group: "Restore", Enabled: s.CanApply(), Visible: true, DisabledReason: "active restore plan has no operations", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) }}}
}
func (s *restoreScreen) Bindings() []Binding {
	return []Binding{{ActionID: "restore.force", Key: "f"}, {ActionID: "restore.detail", Key: "d"}, {ActionID: "restore.apply", Key: "enter"}}
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

func (s *providerScreen) ID() ScreenID { return s.id }
func (s *providerScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !screenKey(key.String()) && key.String() != "tab" {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *providerScreen) Actions() []Action {
	actions := []Action{
		{ID: string(s.id) + ".tab", Label: "Switch saved/changes", Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeyTab}) }},
		{ID: string(s.id) + ".refresh", Label: "Refresh " + screenLabel(s.id), Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'r'}) }},
		{ID: string(s.id) + ".capture", Label: "Capture " + screenLabel(s.id), Enabled: true, Visible: true, Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: 'c'}) }},
	}
	if s.id == ScreenPackages {
		actions = append(actions, Action{ID: string(s.id) + ".toggle", Label: s.ToggleSelectedLabel(), Enabled: s.CanToggleSelected(), Visible: true, DisabledReason: "select an Official, AUR, or Mise package", Run: func() tea.Cmd { return s.Update(tea.KeyPressMsg{Code: tea.KeySpace}) }})
	}
	return actions
}
func (s *providerScreen) Bindings() []Binding {
	bindings := []Binding{{ActionID: string(s.id) + ".tab", Key: "tab"}, {ActionID: string(s.id) + ".refresh", Key: "r"}, {ActionID: string(s.id) + ".capture", Key: "c"}, {Label: "Collapse group", Key: "enter"}, {Label: "Previous group", Key: "[", HideFromFooter: true}, {Label: "Next group", Key: "]", HideFromFooter: true}}
	if s.id == ScreenPackages {
		bindings = append(bindings, Binding{ActionID: string(s.id) + ".toggle", Key: "space"})
	}
	return bindings
}

func (s *configScreen) ID() ScreenID { return ScreenConfig }
func (s *configScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !s.TransientActive() && !screenKey(key.String()) {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}
func (s *configScreen) Actions() []Action {
	policyEnabled := s.CanPolicy()
	return []Action{
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
	return []Binding{{ActionID: "config.policy", Key: "space"}, {ActionID: "config.diff", Key: "d"}, {ActionID: "config.include", Key: "i"}, {ActionID: "config.exclude", Key: "x"}, {ActionID: "config.auto", Key: "a"}, {ActionID: "config.edit", Key: "e"}, {ActionID: "config.open", Key: "o"}, {ActionID: "config.copy", Key: "y"}, {Label: "Previous group", Key: "[", HideFromFooter: true}, {Label: "Next group", Key: "]", HideFromFooter: true}}
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
