package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestPanelFocusUsesFocusedBorderStyle(t *testing.T) {
	styles := NewStyles(ThemePalette{ColorEnabled: true, Border: "1", BorderFocused: "2"})
	blurred := Panel("List", false, 20, 4, "> selected", styles)
	focused := Panel("List", true, 20, 4, "> selected", styles)
	if blurred == focused || !strings.Contains(focused, "> selected") {
		t.Fatalf("focus border was not distinct: blurred=%q focused=%q", blurred, focused)
	}
}

func TestPanelWrapsAnsiStyledProseByCells(t *testing.T) {
	styles := NewStyles(ThemePalette{ColorEnabled: true, Accent: "2"})
	view := Panel("Detail", false, 14, 5, styles.Accent("a long detail sentence"), styles)
	for _, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > 14 {
			t.Fatalf("line is %d cells: %q", width, line)
		}
	}
}
