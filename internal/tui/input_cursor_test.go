package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/charmbracelet/x/ansi"
)

// The search box shows a cursor, and the root keeps delivering its blink
// messages so the cursor flashes rather than freezing.
func TestPaletteSearchCursorIsVisibleAndBlinks(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	updated, blink := m.Update(tea.KeyPressMsg{Code: ':'})
	m = updated.(model)
	if blink == nil {
		t.Fatal("opening the palette started no cursor blink")
	}
	before := m.paletteInput.View()
	if !strings.Contains(before, "\x1b[7m") {
		t.Fatalf("palette search shows no cursor: %q", before)
	}
	updated, next := m.Update(blink())
	m = updated.(model)
	if m.paletteInput.View() == before || next == nil {
		t.Fatal("the palette cursor did not blink")
	}
}

func TestRequestedInputCursorBlinks(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	m, open, _ := m.handleRootMessage(ScreenMachines, components.ModalRequest{Title: "Add machine", Content: "Enter a machine overlay name.", Placeholder: "Machine name"})
	if open == nil {
		t.Fatal("opening an input modal started no cursor blink")
	}
	before := m.requestedModalInput.View()
	if !strings.Contains(ansi.Strip(before), "Machine name") {
		t.Fatalf("placeholder collapsed: %q", before)
	}
	updated, next := m.Update(open())
	m = updated.(model)
	if m.requestedModalInput.View() == before || next == nil {
		t.Fatal("the input modal's cursor did not blink")
	}
}
