package tui

import tea "charm.land/bubbletea/v2"

func keyName(msg tea.Msg) (string, bool) {
	key, ok := msg.(tea.KeyPressMsg)
	return key.String(), ok
}

func isReverseTab(key string) bool { return key == "shift+tab" }
