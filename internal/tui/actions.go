package tui

import tea "charm.land/bubbletea/v2"

type Action struct {
	ID, Label, Group, Keywords string
	Enabled                    bool
	Visible                    bool
	DisabledReason             string
	PaletteInitial             bool
	Run                        func() tea.Cmd
	Screen                     ScreenID
}

// Binding associates a visible key with an action. Actions remain discoverable
// commands even when they intentionally have no direct key binding.
type Binding struct {
	ActionID, Key  string
	FooterPriority int
}

// ActionRegistry is the single source for command discovery and key display.
// Navigation actions can be searchable without crowding an initial palette.
type ActionRegistry struct {
	Actions  []Action
	Bindings []Binding
}

func (r ActionRegistry) Initial() []Action {
	initial := make([]Action, 0, len(r.Actions))
	for _, action := range r.Actions {
		if action.PaletteInitial {
			initial = append(initial, action)
		}
	}
	return initial
}

func (r ActionRegistry) Search(query string) []Action { return filterActions(r.Actions, query) }

func (r ActionRegistry) Key(actionID string) string {
	for _, binding := range r.Bindings {
		if binding.ActionID == actionID {
			return binding.Key
		}
	}
	return ""
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
