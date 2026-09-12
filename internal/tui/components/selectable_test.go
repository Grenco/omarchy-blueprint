package components

import (
	"strings"
	"testing"
)

func TestSelectableKeepsSelectionVisible(t *testing.T) {
	list := Selectable{}
	list.SetSelected(3, 5, 2)
	if list.Selected != 3 || list.offset != 2 {
		t.Fatalf("selection=%d offset=%d", list.Selected, list.offset)
	}
	view := list.View([]string{"a", "b", "c", "d", "e"}, 10, 2)
	if !strings.Contains(strings.TrimSpace(view), "c") || !strings.Contains(strings.TrimSpace(view), "d") {
		t.Fatalf("view=%q", view)
	}
}
