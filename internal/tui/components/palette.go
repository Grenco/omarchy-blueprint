package components

import "strings"

type PaletteItem struct {
	ID, Label, Shortcut, DisabledReason string
	Enabled                             bool
}

func Palette(query string, items []PaletteItem, selected int) string {
	lines := []string{"Command palette: " + query}
	for index, item := range items {
		prefix := " "
		if index == selected {
			prefix = ">"
		}
		line := prefix + " " + item.Label
		if item.Shortcut != "" {
			line += " (" + item.Shortcut + ")"
		}
		if !item.Enabled && item.DisabledReason != "" {
			line += " - " + item.DisabledReason
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
