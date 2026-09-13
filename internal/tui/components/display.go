package components

import (
	"strings"
	"unicode"
)

// DisplayText prevents terminal control characters in external names, paths,
// and errors from changing terminal state or injecting additional rows.
func DisplayText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, value)
}
