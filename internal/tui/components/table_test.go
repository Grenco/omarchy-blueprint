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
	if !strings.Contains(view, "Name") || !strings.Contains(view, "one") || strings.Contains(view, "State") {
		t.Fatalf("responsive table=%q", view)
	}
}
