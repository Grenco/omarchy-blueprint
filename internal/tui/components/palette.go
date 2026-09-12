package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type PaletteItem struct {
	ID, Label, Group, Shortcut, DisabledReason string
	Enabled                                    bool
}

func Palette(query string, items []PaletteItem, selected int) string {
	return "Command palette: " + query + "\n" + PaletteItems(items, selected)
}

func PaletteItems(items []PaletteItem, selected int, styles ...Styles) string {
	style := Styles{}
	if len(styles) > 0 {
		style = styles[0]
	}
	lines := []string{}
	for index, item := range items {
		prefix := " "
		if index == selected && !style.Palette.ColorEnabled {
			prefix = ">"
		}
		line := prefix + " "
		if item.Group != "" {
			line += item.Group + ": "
		}
		line += item.Label
		if item.Shortcut != "" {
			line += " (" + item.Shortcut + ")"
		}
		if !item.Enabled && item.DisabledReason != "" {
			line += " - " + item.DisabledReason
		}
		line = lipgloss.NewStyle().Width(max(1, lipgloss.Width(line))).Render(line)
		if index == selected {
			line = style.Selection(line, true)
		} else if !item.Enabled {
			line = style.Disabled(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
