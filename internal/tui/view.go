package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
)

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
	return components.Statusbar(statusActions(m.activeScreen(), m.bindings()))
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
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", workspace)
	}
	_, ok := m.activeScreen().(DetailView)
	if !ok {
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", workspace)
	}
	innerWidth, innerHeight := components.InteriorSize(layout.detailsWidth, layout.contentHeight)
	details := components.Panel("Details", m.focus == focusDetails, layout.detailsWidth, layout.contentHeight, m.detailScroll.render(m.detailLines(layout), innerWidth, innerHeight), styles)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", workspace, " ", details)
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

func boundedLine(content string, width int) string {
	return lipgloss.NewStyle().Width(max(0, width)).MaxWidth(max(0, width)).Render(content)
}
