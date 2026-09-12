package tui

import tea "charm.land/bubbletea/v2"

// KeyResult makes ownership explicit: a handler either consumes a key or lets
// the next handler in the root routing chain consider it.
type KeyResult struct {
	Consumed bool
	Cmd      tea.Cmd
}

type KeyHandler interface {
	HandleKey(tea.KeyPressMsg) KeyResult
}

func keyName(msg tea.Msg) (string, bool) {
	key, ok := msg.(tea.KeyPressMsg)
	return key.String(), ok
}

func isReverseTab(key string) bool { return key == "shift+tab" }
