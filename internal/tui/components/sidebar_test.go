package components

import (
	"strings"
	"testing"
)

func TestGroupedSidebar(t *testing.T) {
	sections := []NavSection{
		{Items: []NavItem{{ID: "overview", Label: "Overview"}}},
		{Label: "Workflow", Items: []NavItem{{ID: "capture", Label: "Capture"}, {ID: "restore", Label: "Restore"}}},
		{Label: "Software", Items: []NavItem{{ID: "packages", Label: "Packages"}}},
	}
	rendered := RenderSidebar(sections, "packages", 20, Styles{}, true)
	want := []string{
		"  Overview",
		"",
		"WORKFLOW", "    Capture", "    Restore",
		"",
		"SOFTWARE", "  > Packages",
	}
	if len(rendered.Lines) != len(want) {
		t.Fatalf("line count = %d, want %d: %#v", len(rendered.Lines), len(want), rendered.Lines)
	}
	for i, line := range rendered.Lines {
		if got := strings.TrimRight(line, " "); got != want[i] {
			t.Errorf("line %d = %q, want %q", i, got, want[i])
		}
	}
	if rendered.SelectedLine != 7 {
		t.Fatalf("selected line = %d, want 7", rendered.SelectedLine)
	}
}

func TestGroupedSidebarSelectionOnlyAppliesToItems(t *testing.T) {
	sections := []NavSection{{Label: "Workflow", Items: []NavItem{{ID: "capture", Label: "Capture"}}}}
	rendered := RenderSidebar(sections, "workflow", 20, Styles{}, true)
	if rendered.SelectedLine != -1 {
		t.Fatalf("heading selected at line %d", rendered.SelectedLine)
	}
	if strings.Contains(strings.Join(rendered.Lines, "\n"), Icons.Selected) {
		t.Fatalf("heading received selected marker: %#v", rendered.Lines)
	}
}
