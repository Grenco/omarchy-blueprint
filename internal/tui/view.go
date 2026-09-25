package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/charmbracelet/x/ansi"
)

func (m model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		view := tea.NewView("Omarchy Blueprint")
		view.AltScreen = true
		return view
	}
	layout := layoutForSize(m.width, m.height)
	if layout.mode == LayoutTooSmall {
		message := fmt.Sprintf("Terminal too small (%dx%d). Need at least 70 columns and 18 rows.", m.width, m.height)
		view := tea.NewView(strings.Join(components.WrapText(message, m.width), "\n"))
		view.AltScreen = true
		return view
	}
	header := m.header()
	footer := m.styleFooter(m.footer())
	if m.notification != "" {
		footer += "   " + m.color(components.DisplayText(m.notification), m.palette.Success)
	}
	content := m.contentView(layout)
	if m.modal != modalNone {
		content = m.modalView(content, layout)
	}
	rule := m.muted(strings.Repeat("─", max(1, m.width)))
	view := tea.NewView(strings.Join([]string{boundedLine(header, m.width), rule, content, rule, boundedLine(footer, m.width)}, "\n"))
	view.AltScreen = true
	return view
}

func (m model) footer() string {
	if m.modal != modalNone {
		return m.modalFooter()
	}
	bindings := m.bindings()
	if m.focus == focusDetails {
		// Workspace keys do not reach the screen while Details has focus, so
		// the footer offers only pane movement; help still lists every key.
		bindings = m.paneBindings()
	}
	return components.Statusbar(statusActions(m.activeScreen(), bindings))
}

// styleFooter highlights the key in each "key label" hint so hints scan as
// keys first. Footers are built as hints joined by three spaces.
func (m model) styleFooter(footer string) string {
	hints := strings.Split(footer, "   ")
	for i, hint := range hints {
		key, label, ok := strings.Cut(hint, " ")
		if !ok {
			continue
		}
		hints[i] = m.accent(key) + " " + label
	}
	return strings.Join(hints, m.muted(" · "))
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
	if m.session == nil {
		profileName, state = "none", "~ no profile loaded"
	}
	if strings.HasPrefix(state, "!") {
		state = m.warning(state)
	} else if strings.HasPrefix(state, "x") {
		state = m.color(state, m.palette.Error)
	} else if strings.HasPrefix(state, "~") {
		state = m.muted(state)
	}
	sep := m.muted("  │  ")
	brand := "Blueprint"
	if m.palette.ColorEnabled && m.palette.Accent != "" {
		brand = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.palette.Accent)).Render(brand)
	}
	left := brand + sep + m.muted("profile ") + profileName + sep + m.muted("machine ") + machine + sep + state
	return alignHeader(left, m.muted(components.DisplayText(m.profileDir)), m.width)
}

func alignHeader(left, right string, width int) string {
	if right == "" || width <= 0 {
		return left
	}
	available := width - lipgloss.Width(left) - 2
	if available < 12 {
		return left
	}
	right = lipgloss.NewStyle().MaxWidth(available).Render(right)
	gap := max(2, width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
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
	styles := components.NewStyles(m.palette)
	workspace := components.Panel(screenLabel(m.screenID()), m.focus == focusWorkspace, layout.workspaceWidth, layout.contentHeight, m.workspaceContent(layout.workspaceWidth), styles)
	if layout.mode == LayoutCompact {
		// Compact drawers are derived from focus: a focused sidebar or details
		// pane is always drawn over the dimmed workspace, never kept hidden.
		switch {
		case m.focus == focusSidebar:
			return overlayDrawer(workspace, m.navigationPanel(layout, styles), layout.workspaceWidth, true, m.palette.ColorEnabled)
		case m.focus == focusDetails && m.hasDetailsPane():
			return overlayDrawer(workspace, m.detailsPanel(layout, styles), layout.workspaceWidth, false, m.palette.ColorEnabled)
		}
		return workspace
	}
	sidebar := m.navigationPanel(layout, styles)
	if !m.hasDetailsPane() {
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", workspace)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", workspace, " ", m.detailsPanel(layout, styles))
}

// compactNavigationWidth and compactDetailsWidth size the compact drawers so
// part of the workspace stays visible behind them for context.
const compactNavigationWidth = 26

func compactDetailsWidth(workspaceWidth int) int {
	return min(workspaceWidth, max(40, workspaceWidth*3/5))
}

func (m model) navigationPanelWidth() int {
	layout := layoutForSize(m.width, m.height)
	if layout.mode == LayoutCompact {
		return min(compactNavigationWidth, layout.workspaceWidth)
	}
	return layout.sidebarWidth
}

func (m model) detailsPanelWidth() int {
	layout := layoutForSize(m.width, m.height)
	if layout.mode == LayoutCompact {
		return compactDetailsWidth(layout.workspaceWidth)
	}
	return layout.detailsWidth
}

func (m model) detailsInnerWidth() int {
	width, _ := components.InteriorSize(m.detailsPanelWidth(), 0)
	return width
}

func (m model) navigationPanel(layout layout, styles components.Styles) string {
	width := m.navigationPanelWidth()
	rendered := m.sidebarRender(width - 2)
	return components.Panel("Navigation", m.focus == focusSidebar, width, layout.contentHeight, m.sidebarScroll.render(rendered.Lines, width-2, layout.contentHeight-2), styles)
}

func (m model) detailsPanel(layout layout, styles components.Styles) string {
	width := m.detailsPanelWidth()
	innerWidth, innerHeight := components.InteriorSize(width, layout.contentHeight)
	return components.Panel("Details", m.focus == focusDetails, width, layout.contentHeight, m.detailScroll.render(m.detailLines(innerWidth), innerWidth, innerHeight), styles)
}

// overlayDrawer draws drawer over the left or right edge of base, dimming the
// uncovered part of base the same way modal backdrops are dimmed.
func overlayDrawer(base, drawer string, width int, left, color bool) string {
	baseLines, drawerLines := strings.Split(base, "\n"), strings.Split(drawer, "\n")
	for i, line := range baseLines {
		plain := boundedLine(ansi.Strip(line), width)
		if i >= len(drawerLines) {
			baseLines[i] = dimText(plain, color)
			continue
		}
		drawerWidth := lipgloss.Width(drawerLines[i])
		if left {
			baseLines[i] = drawerLines[i] + dimText(ansi.Cut(plain, drawerWidth, width), color)
		} else {
			baseLines[i] = dimText(ansi.Cut(plain, 0, width-drawerWidth), color) + drawerLines[i]
		}
	}
	return strings.Join(baseLines, "\n")
}

func dimText(value string, color bool) string {
	if !color || value == "" {
		return value
	}
	return lipgloss.NewStyle().Faint(true).Render(value)
}

func (m model) workspaceContent(panelWidth int) string {
	width, _ := components.InteriorSize(panelWidth, 0)
	description := components.WrapText(screenInfo(m.screenID()).Short, width)
	styles := components.NewStyles(m.palette)
	for i, line := range description {
		description[i] = styles.SubtleAccent(line)
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
		if responsive, ok := current.(terminalWidthScreen); ok {
			responsive.SetTerminalWidth(m.width)
		}
	}
}

func (m model) sidebarRender(width int) components.SidebarRender {
	return components.RenderSidebar(sidebarSections(), string(m.screenID()), width, components.NewStyles(m.palette), m.focus == focusSidebar)
}

func (m *model) ensureSidebarSelection() {
	layout := layoutForSize(m.width, m.height)
	width := m.navigationPanelWidth()
	_, height := components.InteriorSize(width, layout.contentHeight)
	rendered := m.sidebarRender(width - 2)
	m.sidebarScroll.ensure(rendered.SelectedLine, len(rendered.Lines), height)
}

func (m model) detailLines(width int) []string {
	detailed, ok := m.activeScreen().(DetailView)
	if !ok {
		return nil
	}
	return m.styleDetailLines(components.WrapText(detailed.DetailView(), width))
}

// styleDetailLines gives every screen's Details pane the same hierarchy
// without each DetailView carrying styling: the first line is the heading and
// "Label: value" lines get a muted label.
func (m model) styleDetailLines(lines []string) []string {
	if !m.palette.ColorEnabled {
		return lines
	}
	for i, line := range lines {
		if i == 0 {
			lines[i] = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.palette.Accent)).Render(line)
			continue
		}
		if label, value, ok := strings.Cut(line, ": "); ok && label != "" && len(label) <= 32 && !strings.ContainsAny(label, ".,;") {
			lines[i] = components.NewStyles(m.palette).SubtleAccent(label+":") + " " + value
		}
	}
	return lines
}

func boundedBox(content string, width, height int) string {
	return lipgloss.NewStyle().Width(max(0, width)).Height(max(0, height)).MaxWidth(max(0, width)).MaxHeight(max(0, height)).Render(content)
}

// boundedLine renders content as exactly one line of width cells,
// truncating rather than wrapping so a long header or footer can never add
// rows to the frame.
func boundedLine(content string, width int) string {
	width = max(0, width)
	return lipgloss.NewStyle().Width(width).Render(ansi.Truncate(strings.ReplaceAll(content, "\n", " "), width, "…"))
}
