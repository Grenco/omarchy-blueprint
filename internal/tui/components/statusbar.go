package components

import "strings"

type StatusAction struct {
	Label, Shortcut string
	Enabled         bool
}

func Statusbar(actions []StatusAction) string {
	parts := []string{"? help", ": commands"}
	for _, action := range actions {
		if action.Enabled && action.Shortcut != "" {
			parts = append(parts, action.Shortcut+" "+action.Label)
		}
	}
	return strings.Join(parts, "   ")
}
