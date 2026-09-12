package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type NavItem struct {
	ID, Label string
}

func Sidebar(items []NavItem, selected int, width int) string {
	return SidebarWithStyles(items, selected, width, Styles{})
}

func SidebarWithStyles(items []NavItem, selected int, width int, styles Styles, focused ...bool) string {
	isFocused := true
	if len(focused) > 0 {
		isFocused = focused[0]
	}
	lines := make([]string, 0, len(items))
	for index, item := range items {
		marker := " "
		if index == selected && !styles.Palette.ColorEnabled {
			marker = Icons.Selected
		}
		line := pad(marker+" "+item.Label, width)
		if index == selected && styles.Palette.ColorEnabled {
			line = styles.Selection(line, isFocused)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func pad(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(value)
	}
	return value + strings.Repeat(" ", width-lipgloss.Width(value))
}
