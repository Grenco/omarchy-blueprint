package components

import "strings"

func Help(title string, actions []StatusAction) string {
	lines := []string{"Help: " + title, "j/k or arrows move    Tab cycles focus    Enter activates", ": commands    Esc closes transient UI    q quits", "Symbols: " + Icons.Attention + " attention  " + Icons.Changed + " changed  " + Icons.Ready + " ready  " + Icons.Error + " error  " + Icons.Unknown + " unknown"}
	for _, action := range actions {
		if action.Enabled && action.Shortcut != "" {
			lines = append(lines, action.Shortcut+"  "+action.Label)
		}
	}
	return strings.Join(lines, "\n")
}
