package tui

import tea "charm.land/bubbletea/v2"

type Action struct {
	ID, Label, Group, Keywords, Shortcut string
	Enabled                              bool
	Visible                              bool
	DisabledReason                       string
	FooterPriority                       int
	Run                                  func() tea.Cmd
	Screen                               ScreenID
}

func visibleActions(actions []Action) []Action {
	visible := make([]Action, 0, len(actions))
	for _, action := range actions {
		if action.Visible {
			visible = append(visible, action)
		}
	}
	return visible
}

func subsequenceMatch(query, value string) bool {
	query, value = lower(query), lower(value)
	for _, r := range value {
		if len(query) > 0 && r == rune(query[0]) {
			query = query[1:]
		}
	}
	return query == ""
}

func lower(s string) string {
	result := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		result = append(result, r)
	}
	return string(result)
}

func filterActions(actions []Action, query string) []Action {
	filtered := make([]Action, 0, len(actions))
	for _, action := range actions {
		if subsequenceMatch(query, action.Label) || subsequenceMatch(query, action.ID) || subsequenceMatch(query, action.Group) || subsequenceMatch(query, action.Keywords) {
			filtered = append(filtered, action)
		}
	}
	return filtered
}
