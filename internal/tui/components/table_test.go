package components

import (
	"strings"
	"testing"
)

func TestTableEnsureAndViewport(t *testing.T) {
	table := Table{}
	table.Ensure(3, 5, 2)
	if table.Offset != 2 {
		t.Fatalf("offset=%d", table.Offset)
	}
	if got := table.View([]string{"a", "b", "c", "d", "e"}, 2); got != "c\nd" {
		t.Fatalf("view=%q", got)
	}
}

func TestTableRenderHidesTrailingColumnsAndKeepsHeader(t *testing.T) {
	table := Table{}
	view := table.Render([]Column{{Title: "Name", MinWidth: 4}, {Title: "Path", MinWidth: 8}, {Title: "State", MinWidth: 8}}, []Row{{Cells: []string{"one", "/long/path", "ready"}, Selected: true}}, 13, 3, Styles{})
	if !strings.Contains(view, "Name") || !strings.Contains(view, "on") || strings.Contains(view, "State") {
		t.Fatalf("responsive table=%q", view)
	}
}

func TestTableRenderKeepsLogicalRowsSingleLine(t *testing.T) {
	table := Table{}
	view := table.Render([]Column{{Title: "Path", MinWidth: 4}, {Title: "State", Width: 32, MinWidth: 8}, {Title: "Policy", MinWidth: 6}}, []Row{{Cells: []string{"foo", "Changed from Omarchy default", "Included"}}}, 60, 3, Styles{})
	lines := strings.Split(view, "\n")
	if len(lines) != 2 || strings.Contains(view, "default\n") {
		t.Fatalf("wrapped table row=%q", view)
	}
	for _, line := range lines {
		if len(line) > 60 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
}

func TestTableRenderSectionRowRunsDividerAcrossTable(t *testing.T) {
	table := Table{}
	view := table.Render(
		[]Column{{Title: "ITEM", MinWidth: 12}, {Title: "STATE", MinWidth: 8}},
		[]Row{{Cells: []string{"▼ Official packages"}, Divider: true}},
		50,
		3,
		Styles{},
	)
	lines := strings.Split(view, "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "▼ Official packages ─────") || len([]rune(lines[1])) != 50 {
		t.Fatalf("section divider did not preserve the table shape: %q", view)
	}
}

func TestTableRenderSectionDividerUsesMutedColour(t *testing.T) {
	styles := NewStyles(ThemePalette{Muted: "#777777", ColorEnabled: true})
	view := (&Table{}).Render(
		[]Column{{Title: "ITEM", MinWidth: 12}, {Title: "STATE", MinWidth: 8}},
		[]Row{{Cells: []string{"Official packages"}, Divider: true}},
		50,
		3,
		styles,
	)
	if !strings.Contains(view, styles.Muted(strings.Repeat("─", 32))) {
		t.Fatalf("section divider rule did not use the muted style: %q", view)
	}
}
