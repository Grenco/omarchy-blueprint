package components

import "strings"

type PaletteItem struct {
	ID, Label, Group, Shortcut, DisabledReason string
	Enabled                                    bool
}

func Palette(query string, items []PaletteItem, selected int) string {
	return "Command palette: " + query + "\n" + PaletteItems(items, selected)
}

func PaletteItems(items []PaletteItem, selected int) string {
	lines := []string{}
	for index, item := range items {
		prefix := " "
		if index == selected {
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
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
