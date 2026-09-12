package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// TabBar renders an explicit active tab without screen-specific markers.
func TabBar(tabs []string, active string, styles Styles) string {
	rendered := make([]string, 0, len(tabs))
	for _, tab := range tabs {
		if tab == active {
			if styles.Palette.ColorEnabled {
				rendered = append(rendered, lipgloss.NewStyle().Foreground(lipgloss.Color(styles.Palette.Accent)).Bold(true).Underline(true).Render("[ "+tab+" ]"))
			} else {
				rendered = append(rendered, "[active] "+tab)
			}
			continue
		}
		rendered = append(rendered, styles.Muted(tab))
	}
	return strings.Join(rendered, "   ")
}
