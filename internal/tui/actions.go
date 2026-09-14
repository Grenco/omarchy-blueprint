package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

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
	// ActionID is optional. Bindings without an action are still shown in the
	// footer and help, but do not imply a palette command.
	ActionID, Label, Key, Context string
	Keys                          []string
	FooterPriority                int
	HideFromFooter                bool
}

// ActionRegistry is the single source for command discovery and key display.
// Navigation actions can be searchable without crowding an initial palette.
type ActionRegistry struct {
	Actions  []Action
	Bindings []Binding
}

type HelpEntry struct {
	Key, Label, Context, Group, ActionID, Keywords string
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

func (r ActionRegistry) SearchBindings(query string) []HelpEntry {
	actions := make(map[string]Action, len(r.Actions))
	for _, action := range r.Actions {
		actions[action.ID] = action
	}
	entries := make([]HelpEntry, 0, len(r.Bindings))
	for _, binding := range r.Bindings {
		action := actions[binding.ActionID]
		label := binding.Label
		if label == "" {
			label = action.Label
		}
		entry := HelpEntry{Key: binding.DisplayKeys(), Label: label, Context: binding.Context, Group: action.Group, ActionID: action.ID, Keywords: action.Keywords}
		if searchMatch(query, entry.Key, entry.Label, entry.Context, entry.Group, entry.ActionID, entry.Keywords) {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (r ActionRegistry) Key(actionID string) string {
	for _, binding := range r.Bindings {
		if binding.ActionID == actionID {
			return binding.Key
		}
	}
	return ""
}

func (b Binding) DisplayKeys() string {
	if len(b.Keys) > 0 {
		return strings.Join(b.Keys, " / ")
	}
	return b.Key
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
	exact, prefix, substring := make([]Action, 0, len(actions)), make([]Action, 0, len(actions)), make([]Action, 0, len(actions))
	for _, action := range actions {
		rank := searchRank(query, action.Label, action.ID, action.Group, action.Keywords)
		if rank == 0 {
			exact = append(exact, action)
		} else if rank == 1 {
			prefix = append(prefix, action)
		} else if rank == 2 {
			substring = append(substring, action)
		}
	}
	return append(append(exact, prefix...), substring...)
}

func searchMatch(query string, values ...string) bool { return searchRank(query, values...) < 3 }
func searchRank(query string, values ...string) int {
	query = lower(strings.TrimSpace(query))
	if query == "" {
		return 0
	}
	text := lower(strings.Join(values, " "))
	for _, word := range strings.Fields(query) {
		if !strings.Contains(text, word) {
			return 3
		}
	}
	for _, value := range values {
		if query == lower(value) || keywordMatch(query, value) {
			return 0
		}
	}
	if strings.HasPrefix(text, query) {
		return 1
	}
	return 2
}

func keywordMatch(query, keywords string) bool {
	for _, keyword := range strings.Fields(keywords) {
		if lower(query) == lower(keyword) {
			return true
		}
	}
	return false
}
