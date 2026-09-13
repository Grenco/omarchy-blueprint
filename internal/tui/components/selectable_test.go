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

func TestSelectableVimNavigation(t *testing.T) {
	list := Selectable{Selected: 5}
	list.Vim("g", 10, 4)
	list.Vim("g", 10, 4)
	if list.Selected != 0 {
		t.Fatalf("gg selected=%d", list.Selected)
	}
	list.Vim("G", 10, 4)
	if list.Selected != 9 {
		t.Fatalf("G selected=%d", list.Selected)
	}
	list.Vim("ctrl+u", 10, 4)
	if list.Selected != 7 {
		t.Fatalf("ctrl+u selected=%d", list.Selected)
	}
	list.Vim("ctrl+d", 10, 4)
	if list.Selected != 9 {
		t.Fatalf("ctrl+d selected=%d", list.Selected)
	}
}
