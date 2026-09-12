package tui

import "github.com/Grenco/omarchy-blueprint/internal/tui/components"

type themeTickMsg struct{}

type handoffFinishedMsg struct {
	Kind string
	Err  error
}

type clipboardCopiedMsg struct{}

// pathInspectedMsg is the browser's asynchronous inspection result.
type pathInspectedMsg = components.BrowserInspectionMsg
