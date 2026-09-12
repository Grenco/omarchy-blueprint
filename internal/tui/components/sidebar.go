package components

import "strings"

type NavItem struct {
	ID, Label string
}

func Sidebar(items []NavItem, selected int, width int) string {
	lines := make([]string, 0, len(items))
	for index, item := range items {
		marker := " "
		if index == selected {
			marker = ">"
		}
		lines = append(lines, pad(marker+" "+item.Label, width))
	}
	return strings.Join(lines, "\n")
}

func pad(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(value) > width {
		return value[:width]
	}
	return value + strings.Repeat(" ", width-len(value))
}
