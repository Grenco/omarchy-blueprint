package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	lines := panelWrap(content, inner)
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

// InteriorSize is the space available to a panel child after its border.
func InteriorSize(width, height int) (int, int) { return max(0, width-2), max(0, height-2) }

func panelWrap(content string, width int) []string {
	if width <= 0 {
		return nil
	}
	lines := []string{}
	for _, line := range strings.Split(content, "\n") {
		lines = append(lines, strings.Split(ansi.Wrap(line, width, " "), "\n")...)
	}
	return lines
}

func WrapText(content string, width int) []string { return panelWrap(content, width) }

func panelTruncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(value)
}
