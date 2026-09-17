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

func TestSelectableJumpToAnchor(t *testing.T) {
	anchors := []int{0, 2, 6}
	for _, test := range []struct {
		name           string
		selected       int
		forward, moved bool
		want           int
	}{
		{"child backward", 3, false, true, 2},
		{"header backward", 2, false, true, 0},
		{"child forward", 3, true, true, 6},
		{"first header backward", 0, false, false, 0},
		{"final group forward", 6, true, false, 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			list := Selectable{Selected: test.selected}
			if got := list.JumpToAnchor(anchors, test.forward, 8, 2); got != test.moved || list.Selected != test.want {
				t.Fatalf("moved=%v selected=%d, want moved=%v selected=%d", got, list.Selected, test.moved, test.want)
			}
			if test.moved && (list.Selected < list.offset || list.Selected >= list.offset+2) {
				t.Fatalf("destination %d is outside viewport at offset %d", list.Selected, list.offset)
			}
		})
	}
}
