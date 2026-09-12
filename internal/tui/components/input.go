package components

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// TextInputModal provides the common input state used by root-owned modals.
type TextInputModal struct{ Input textinput.Model }

func NewTextInputModal(value, placeholder string) TextInputModal {
	input := textinput.New()
	input.SetValue(value)
	input.Placeholder = placeholder
	return TextInputModal{Input: input}
}

func (m *TextInputModal) Focus() tea.Cmd { return m.Input.Focus() }
func (m *TextInputModal) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.Input, cmd = m.Input.Update(msg)
	return cmd
}
func (m TextInputModal) Value() string { return m.Input.Value() }
func (m TextInputModal) View() string  { return m.Input.View() }
