package profilegit

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

var urlUserinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/?#\s@]*@`)

func gitError(action string, err error) error {
	var runErr *command.RunError
	if errors.As(err, &runErr) {
		return fmt.Errorf("%s: %s", action, SanitizeRemote(runErr.Output))
	}
	return fmt.Errorf("%s: %s", action, SanitizeRemote(err.Error()))
}

// SanitizeDisplay removes terminal controls from untrusted Git values.
func SanitizeDisplay(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 0x20 && r != 0x7f && !(r >= 0x80 && r <= 0x9f) && !unicode.IsControl(r) {
			return r
		}
		return '?'
	}, value)
}
