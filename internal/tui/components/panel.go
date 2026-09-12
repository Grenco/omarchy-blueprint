package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Panel renders a cell-bounded Unicode border around content.
func Panel(title string, focused bool, width, height int, content string, styles Styles) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if width < 3 || height < 3 {
		return lipgloss.NewStyle().Width(width).Height(height).Render(content)
	}
	inner := width - 2
	label := " " + title + " "
	label = panelTruncate(label, inner)
	top := "┌" + label + strings.Repeat("─", max(0, inner-lipgloss.Width(label))) + "┐"
	lines := strings.Split(content, "\n")
	rows := make([]string, 0, height)
	rows = append(rows, styles.Border(top, focused))
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(lines) {
			line = panelTruncate(lines[i], inner)
		}
		rows = append(rows, styles.Border("│", focused)+lipgloss.NewStyle().Width(inner).MaxWidth(inner).Render(line)+styles.Border("│", focused))
	}
	rows = append(rows, styles.Border("└"+strings.Repeat("─", inner)+"┘", focused))
	return strings.Join(rows, "\n")
}

func panelTruncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(value)
}
