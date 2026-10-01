package screens

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
)

func activationLabel(mode policy.ActivationMode) string {
	switch mode {
	case policy.ActivationRestoreWorkingState:
		return "Restore working state"
	case policy.ActivationReview:
		return "Review activation"
	default:
		return "Persistent state only"
	}
}

func (s *Restore) updateActivationReview(key string) tea.Cmd {
	switch key {
	case "esc":
		s.reviewingActivation = false
	case "j", "down":
		s.activationSelected = min(s.activationSelected+1, len(s.current.ActivationReview)-1)
	case "k", "up":
		s.activationSelected = max(0, s.activationSelected-1)
	case "space", " ":
		if len(s.current.ActivationReview) == 0 {
			return nil
		}
		unit := s.current.ActivationReview[s.activationSelected].Unit
		for i, selected := range s.activationChoices {
			if selected == unit {
				s.activationChoices = append(s.activationChoices[:i], s.activationChoices[i+1:]...)
				return nil
			}
		}
		s.activationChoices = append(s.activationChoices, unit)
	case "enter":
		s.options.ReviewActivation = &policy.ActivationReviewSelection{Units: append([]string(nil), s.activationChoices...)}
		s.override, s.reviewingActivation = true, false
		return s.refreshPlan()
	}
	return nil
}

func (s *Restore) activationReviewView() string {
	lines := []string{"Review activation", "Space: approve/unapprove this start · j/k: move · Enter: finish and replan · Esc: cancel", ""}
	start := max(0, s.activationSelected-max(1, s.height-5)/2)
	end := min(len(s.current.ActivationReview), start+max(1, s.height-5))
	if s.height <= 0 {
		start, end = 0, len(s.current.ActivationReview)
	}
	for i := start; i < end; i++ {
		candidate := s.current.ActivationReview[i]
		mark, cursor := "[ ]", "  "
		for _, selected := range s.activationChoices {
			if selected == candidate.Unit {
				mark = "[x]"
			}
		}
		if i == s.activationSelected {
			cursor = "> "
		}
		lines = append(lines, cursor+mark+" "+components.DisplayText(candidate.Unit))
	}
	return strings.Join(lines, "\n")
}
