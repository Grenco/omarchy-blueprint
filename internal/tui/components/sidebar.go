package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type NavItem struct {
	ID, Label string
}

// NavSection groups navigation items under a non-selectable heading.
type NavSection struct {
	Label string
	Items []NavItem
}

// SidebarRender keeps rendered rows aligned with the selectable navigation item.
type SidebarRender struct {
	Lines        []string
	SelectedLine int
}

// RenderSidebar renders grouped navigation headings without making them selectable.
func RenderSidebar(sections []NavSection, selectedID string, width int, styles Styles, focused bool) SidebarRender {
	rendered := SidebarRender{SelectedLine: -1}
	for _, section := range sections {
		if section.Label != "" {
			if len(rendered.Lines) > 0 {
				rendered.Lines = append(rendered.Lines, "")
			}
			rendered.Lines = append(rendered.Lines, pad(styles.Muted(strings.ToUpper(section.Label)), width))
		}
		indent := ""
		if section.Label != "" {
			indent = "  "
		}
		for _, item := range section.Items {
			marker := " "
			if item.ID == selectedID && !styles.Palette.ColorEnabled {
				marker = Icons.Selected
			}
			line := pad(indent+marker+" "+item.Label, width)
			if item.ID == selectedID {
				rendered.SelectedLine = len(rendered.Lines)
				if styles.Palette.ColorEnabled {
					line = styles.Selection(line, focused)
				}
			}
			rendered.Lines = append(rendered.Lines, line)
		}
	}
	return rendered
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
