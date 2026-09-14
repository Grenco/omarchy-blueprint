package components

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TextInputModal provides the common input state used by root-owned modals.
type TextInputModal struct {
	Input         textinput.Model
	rawValue      string
	renderedValue string
}

func NewTextInputModal(value, placeholder string) TextInputModal {
	input := textinput.New()
	input.SetValue(value)
	input.Placeholder = placeholder
	return TextInputModal{Input: input, rawValue: value, renderedValue: input.Value()}
}

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
func (m TextInputModal) View() string { return DisplayText(ansi.Strip(m.Input.View())) }
