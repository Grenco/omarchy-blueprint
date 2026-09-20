package tui

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
)

// handleRootMessage is the single dispatcher for root-owned event families.
// It is called both for messages that arrive directly at the root and for
// messages wrapped with their originating screen; handled reports whether
// the root consumed msg, in which case it must not also reach a screen.
func (m model) handleRootMessage(origin ScreenID, msg tea.Msg) (model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case showHelpMsg:
		m.openModal(modalHelp)
		return m, nil, true
	case components.ModalRequest:
		if origin == "" {
			return m, nil, false
		}
		m.requestedModal = &ModalRequest{
			Title:       msg.Title,
			Content:     msg.Content,
			Input:       msg.Input,
			Placeholder: msg.Placeholder,
		}
		m.requestedModalScreen = origin
		m.openModal(modalConfirm)
		if msg.Input != "" || msg.Placeholder != "" {
			m.requestedModalInput = components.NewTextInputModal(msg.Input, msg.Placeholder)
			return m, m.requestedModalInput.Focus(), true
		}
		return m, nil, true
	case screens.OverviewTarget:
		updated, cmd := m.handleOverviewTarget(msg)
		return updated.(model), cmd, true
	case screens.CaptureComplete:
		updated, cmd := m.handleCaptureComplete(msg)
		return updated.(model), cmd, true
	case screens.SessionReloadNeeded:
		updated, cmd := m.reloadSessionScreens(msg.Reason)
		return updated.(model), cmd, true
	case screens.HandoffRequest:
		updated, cmd := m.handleHandoffRequest(msg)
		return updated.(model), cmd, true
	case handoffFinishedMsg:
		updated, cmd := m.handleHandoffFinished(msg)
		return updated.(model), cmd, true
	case screens.MachineMutationComplete:
		if msg.Err == nil {
			m.notification = msg.Notice
		} else {
			m.notification = "Machine update failed: " + msg.Err.Error()
		}
		// Machines.Update also consumes this message to clear its own busy
		// state and re-clamp selection, so it must still reach the origin
		// screen in addition to the root notification set above.
		var originCmd tea.Cmd
		if origin != "" {
			if current := m.screens[origin]; current != nil {
				originCmd = wrapScreenCmd(origin, current.Update(msg))
			}
		}
		if msg.Err == nil {
			return m, tea.Batch(originCmd, m.refreshInitializedAuthorityScreens(origin)), true
		}
		return m, originCmd, true
	case screens.AuthorityChanged:
		m.notification = msg.Notice
		return m, m.refreshInitializedAuthorityScreens(""), true
	case screens.PolicyNavigation:
		id := ScreenID(msg.Category)
		cmd := m.selectScreen(id)
		switch current := m.screens[id].(type) {
		case *providerScreen:
			current.ShowPolicy()
		case *configScreen:
			current.ShowPolicy()
		case *resourcesScreen:
			current.ShowPolicy()
		}
		return m, cmd, true
	case screens.Notice:
		m.notification = msg.Message
		return m, nil, true
	}
	return m, nil, false
}

func (m model) refreshInitializedAuthorityScreens(exclude ScreenID) tea.Cmd {
	commands := make([]tea.Cmd, 0, len(m.screens))
	for id, current := range m.screens {
		if id == exclude || !m.initialized[id] {
			continue
		}
		if initializable, ok := current.(initializableScreen); ok {
			commands = append(commands, wrapScreenCmd(id, initializable.Init()))
		}
	}
	return tea.Batch(commands...)
}

// handleOverviewTarget is the canonical Overview deep-link navigation
// behavior, shared by direct and origin-wrapped OverviewTarget messages.
func (m model) handleOverviewTarget(target screens.OverviewTarget) (tea.Model, tea.Cmd) {
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

// handleHandoffFinished is the canonical external-tool handoff completion
// behavior: LazyGit triggers a full session reload, other tools refresh the
// screen that requested the handoff.
func (m model) handleHandoffFinished(finished handoffFinishedMsg) (tea.Model, tea.Cmd) {
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

// handleCaptureComplete is the canonical Capture-completion refresh
// behavior: it notifies the user and refreshes Capture, Overview, Sync, and
// the captured category screens.
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

// reloadSessionScreens is the canonical session-reload behavior after a
// profile Git pull or LazyGit handoff: it reloads the shared session and
// re-initializes every screen that has already been visited.
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

// handleHandoffRequest is the canonical external-tool handoff dispatch
// behavior for copy/editor/file-manager/browser/LazyGit requests.
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
