package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

func TestTUIActivationModesAreExplicitAndIndependent(t *testing.T) {
	screen := NewRestore(nil)
	for _, want := range []policy.ActivationMode{policy.ActivationRestoreWorkingState, policy.ActivationReview, policy.ActivationPersistentOnly} {
		screen.Update(tea.KeyPressMsg{Code: 'a'})
		if screen.options.ActivationMode() != want || screen.options.Conflicts != policy.ConflictSafe || screen.options.Convergence != policy.ConvergenceAdditive {
			t.Fatalf("mode changed another axis: %+v", screen.options)
		}
	}
}

func TestTUIReviewActivationRequiresIndividualSelection(t *testing.T) {
	screen := NewRestore(nil)
	screen.options.Activation, screen.override = policy.ActivationReview, true
	screen.current.ActivationMode = "review"
	screen.current.ActivationReview = []model.ActivationCandidate{{Provider: "services", Resource: "backup.service", Unit: "backup.service"}, {Provider: "services", Resource: "watch.path", Unit: "watch.path"}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !screen.reviewingActivation || len(screen.activationChoices) != 0 || !strings.Contains(screen.View(), "[ ] backup.service") {
		t.Fatalf("review did not begin unselected: %s", screen.View())
	}
	screen.Update(tea.KeyPressMsg{Code: ' '})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if screen.reviewingActivation || screen.options.ReviewActivation == nil || len(screen.options.ReviewActivation.Units) != 1 || screen.options.ReviewActivation.Units[0] != "backup.service" || screen.confirm {
		t.Fatalf("individual review selected unintended starts or applied early: %+v", screen.options)
	}
}
