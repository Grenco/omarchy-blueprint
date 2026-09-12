package components

import (
	"strings"
	"testing"
)

func TestPanelFocusUsesFocusedBorderStyle(t *testing.T) {
	styles := NewStyles(ThemePalette{ColorEnabled: true, Border: "1", BorderFocused: "2"})
	blurred := Panel("List", false, 20, 4, "> selected", styles)
	focused := Panel("List", true, 20, 4, "> selected", styles)
	if blurred == focused || !strings.Contains(focused, "> selected") {
		t.Fatalf("focus border was not distinct: blurred=%q focused=%q", blurred, focused)
	}
}
