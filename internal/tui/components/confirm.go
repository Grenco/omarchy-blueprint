package components

// ModalRequest asks the root layout to show a transient overlay while the
// originating screen retains ownership of Enter/Escape.
type ModalRequest struct {
	Title, Content, Input, Placeholder string
}

// TextInputSubmitted returns a root-owned modal value to its requesting screen.
type TextInputSubmitted struct{ Value string }

// Confirm returns a confirmation dialog's body text. The enter/esc hint is
// owned by the modal footer (see modals.go's modalFooter), so it is not
// repeated here.
func Confirm(prompt string) string {
	return prompt
}
