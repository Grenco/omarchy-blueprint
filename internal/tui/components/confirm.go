package components

// ModalRequest asks the root layout to show a transient overlay while the
// originating screen retains ownership of Enter/Escape.
type ModalRequest struct {
	Title, Content string
}

func Confirm(prompt string) string {
	return prompt + " [Enter confirm, Esc cancel]"
}
