package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTUIModelRendersTitleInAlternateScreen(t *testing.T) {
	view := (model{}).View()
	if view.Content != "Omarchy Blueprint" {
		t.Errorf("content = %q", view.Content)
	}
	if !view.AltScreen {
		t.Error("alternate screen is disabled")
	}
}

func TestTUIModelQuitsOnQAndCtrlC(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 'q'}, {Code: 'c', Mod: tea.ModCtrl}} {
		_, cmd := (model{}).Update(key)
		if cmd == nil || cmd() != tea.Quit() {
			t.Errorf("key %q did not quit", key.String())
		}
	}
}
