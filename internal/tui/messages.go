package tui

import "github.com/Grenco/omarchy-blueprint/internal/tui/components"

type themeTickMsg struct{}

type handoffFinishedMsg struct {
	Refresh ScreenID
	Kind    string
	Err     error
}

type clipboardCopiedMsg struct{}

type showHelpMsg struct{}

// screenMsg preserves the destination of asynchronous screen work after focus changes.
type screenMsg struct {
	Screen ScreenID
	Msg    any
}

// pathInspectedMsg is the browser's asynchronous inspection result.
type pathInspectedMsg = components.BrowserInspectionMsg
