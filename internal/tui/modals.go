package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/charmbracelet/x/ansi"
)

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

// modalFooter is the canonical footer text for the active modal.
func (m model) modalFooter() string {
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
		return m.welcomeFooter()
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

// updateRequestedModal handles input/submit/cancel for a screen-requested
// confirmation or text-input modal. The response always returns to
// requestedModalScreen, never to whichever screen happens to be active.
func (m model) updateRequestedModal(msg tea.Msg, key string, isKey bool) (model, tea.Cmd, bool) {
	if m.requestedModal == nil {
		return m, nil, false
	}
	if (m.requestedModal.Input != "" || m.requestedModal.Placeholder != "") && isKey {
		switch key {
		case "esc":
			screenID := m.requestedModalScreen
			m.closeModal()
			_, cmd := m.updateScreenMsg(screenMsg{
				Screen: screenID,
				Msg:    tea.KeyPressMsg{Code: tea.KeyEsc},
			})
			return m, cmd, true
		case "enter":
			screenID, value := m.requestedModalScreen, m.requestedModalInput.Value()
			m.closeModal()
			_, cmd := m.updateScreenMsg(screenMsg{
				Screen: screenID,
				Msg:    components.TextInputSubmitted{Value: value},
			})
			return m, cmd, true
		default:
			return m, m.requestedModalInput.Update(msg), true
		}
	}
	if isKey && (key == "enter" || key == "esc") {
		screenID := m.requestedModalScreen
		code := tea.KeyEsc
		if key == "enter" {
			code = tea.KeyEnter
		}
		m.closeModal()
		_, cmd := m.updateScreenMsg(screenMsg{
			Screen: screenID,
			Msg:    tea.KeyPressMsg{Code: code},
		})
		return m, cmd, true
	}
	return m, nil, false
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
	dim := func(value string) string {
		if !color || value == "" {
			return value
		}
		return lipgloss.NewStyle().Faint(true).Render(value)
	}
	for row := 0; row < height; row++ {
		// Strip the base row's own styling before dimming it: wrapping an
		// already-styled string in another style does not survive resets
		// embedded in the original (see presentation.go's styledDecision for
		// the same nesting issue applied to table cells). dim is applied last,
		// and only to plain text, so it is never itself the thing re-wrapped.
		plain := boundedLine(ansi.Strip(baseLines[row]), width)
		if row < y || row >= y+overlayHeight {
			baseLines[row] = dim(plain)
			continue
		}
		// Panels use bounded ASCII/Unicode cells; retain the base margins and
		// replace the central rectangle without adding rows to the base.
		overlayRow := overlayLines[row-y]
		right := x + lipgloss.Width(overlayRow)
		prefix := ansi.Cut(plain, 0, x)
		suffix := ansi.Cut(plain, right, width)
		baseLines[row] = dim(prefix) + overlayRow + dim(suffix)
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
		width = max(40, min(width, 80))
		innerWidth, _ = components.InteriorSize(width, height)
		overlay = m.welcomeContent(innerWidth)
	case modalConfirm:
		if m.requestedModal != nil {
			title, overlay = m.requestedModal.Title, m.requestedModal.Content
			if m.requestedModal.Input != "" || m.requestedModal.Placeholder != "" {
				overlay += "\n\n" + m.requestedModalInput.View()
			}
		}
	}
	switch m.modal {
	case modalConfirm:
		width = max(40, min(width, 68))
		height = max(5, min(height, len(strings.Split(overlay, "\n"))+4))
	case modalWelcome:
		height = max(5, min(height, len(components.WrapText(overlay, innerWidth))+3))
	}
	overlay = components.Panel(title, true, width, height, overlay, styles)
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
