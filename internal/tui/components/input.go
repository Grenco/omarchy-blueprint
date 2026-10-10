package components

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// TextInputModal provides the common input state used by root-owned modals.
type TextInputModal struct {
	Input         textinput.Model
	rawValue      string
	renderedValue string
}

func NewTextInputModal(value, placeholder string) TextInputModal {
	input := textinput.New()
	input.SetStyles(inputStyles())
	input.SetValue(value)
	input.Placeholder = placeholder
	return TextInputModal{Input: input, rawValue: value, renderedValue: input.Value()}
}

// inputStyles uses attributes rather than colours (a faint placeholder and
// a reverse-video cursor), so inputs look right in any theme and under
// NO_COLOR while still showing where to type.
func inputStyles() textinput.Styles {
	plain := textinput.StyleState{
		Text:        lipgloss.NewStyle(),
		Placeholder: lipgloss.NewStyle().Faint(true),
		Suggestion:  lipgloss.NewStyle().Faint(true),
		Prompt:      lipgloss.NewStyle(),
	}
	styles := textinput.Styles{Focused: plain, Blurred: plain}
	styles.Cursor.Blink = true
	return styles
}

// Focus focuses the input and starts its cursor blinking; the returned
// command must reach Update for the blink to continue.
func (m *TextInputModal) Focus() tea.Cmd { return m.Input.Focus() }
func (m *TextInputModal) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.Input, cmd = m.Input.Update(msg)
	return cmd
}
func (m TextInputModal) Value() string {
	if m.Input.Value() == m.renderedValue {
		return m.rawValue
	}
	return m.Input.Value()
}

// View renders the input with its cursor. It renders a copy whose text and
// placeholder have been made safe for display, so the only escape
// sequences in the result are the input's own styling. Without a width the
// input would show only a placeholder's first character, which reads like a
// typed letter; the copy gets one wide enough for the whole placeholder.
func (m TextInputModal) View() string {
	display := m.Input
	if value := display.Value(); value != "" {
		position := display.Position()
		display.SetValue(DisplayText(value))
		display.SetCursor(position)
	}
	display.Placeholder = DisplayText(display.Placeholder)
	if display.Value() == "" && display.Placeholder != "" && display.Width() == 0 {
		display.SetWidth(lipgloss.Width(display.Placeholder))
	}
	return display.View()
}
