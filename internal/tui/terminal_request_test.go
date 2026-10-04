package tui

import (
	"io"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
)

type fakeTerminalCommand struct{}

func (fakeTerminalCommand) Run() error          { return nil }
func (fakeTerminalCommand) SetStdin(io.Reader)  {}
func (fakeTerminalCommand) SetStdout(io.Writer) {}
func (fakeTerminalCommand) SetStderr(io.Writer) {}

type terminalDone struct{ err error }

// A screen's terminal request travels like every screen command: wrapped
// with its screen. The root must turn it into a real, unwrapped exec so
// Bubble Tea releases the terminal; wrapping the exec itself (the bug a
// fresh-machine Restore hit) leaves the screen waiting forever.
func TestScreenTerminalRequestReachesBubbleTeaUnwrapped(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	request := screens.TerminalRequest{Command: fakeTerminalCommand{}, Done: func(err error) tea.Msg { return terminalDone{err} }}
	wrapped := wrapScreenCmd(ScreenRestore, func() tea.Msg { return request })()
	if _, ok := wrapped.(screenMsg); !ok {
		t.Fatalf("screen command was not routed through its screen: %#v", wrapped)
	}
	_, cmd := m.Update(wrapped)
	if cmd == nil {
		t.Fatal("root ignored the terminal request")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("terminal request produced no exec")
	} else if _, wrappedAgain := msg.(screenMsg); wrappedAgain {
		t.Fatalf("root wrapped the exec back to the screen, so Bubble Tea never runs it: %#v", msg)
	}
}
