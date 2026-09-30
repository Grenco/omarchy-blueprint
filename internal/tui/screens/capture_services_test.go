package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestCaptureReviewSelectsServiceWithoutPersistingPolicy(t *testing.T) {
	f := newReviewFixture(t, workflow.TargetInspection{Key: "backup.service", Label: "backup.service · Custom user service", Current: workflow.TargetPresent, Desired: workflow.TargetUnknown, CaptureEligible: true, RequiresSelection: true, Fingerprint: "content-a"})
	if err := f.session.SetProviders([]workflow.Provider{reviewProvider{id: "services", targets: &f.targets, captured: &f.captured}}); err != nil {
		t.Fatal(err)
	}
	f.screen.statuses = []workflow.ProviderStatus{{ID: "services"}}
	f.openReview(t)
	if view := f.screen.View(); !strings.Contains(view, "Not selected") {
		t.Fatalf("new service did not begin unselected:\n%s", view)
	}
	f.press(t, tea.KeyPressMsg{Code: tea.KeySpace})
	if selected := f.screen.review.inspection.Categories["services"][0]; !selected.Selected || selected.Outcome != workflow.CaptureOutcomeAdd {
		t.Fatalf("Space changed policy rather than reviewed selection: %+v", selected)
	}
	if rules := f.session.Profile().Policy.Capture; len(rules) != 0 {
		t.Fatalf("selection was persisted as Capture policy: %+v", rules)
	}
	if modal := f.press(t, tea.KeyPressMsg{Code: tea.KeyEnter}); modal == nil {
		t.Fatal("reviewed Services selection did not request approval")
	}
	f.press(t, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(f.captured) != 1 || !f.captured[0].Targets["backup.service"].Selected {
		t.Fatalf("approval did not pass selected unit to workflow: %+v", f.captured)
	}
}

func TestCaptureReviewKeepsAdvancedServiceCandidatesHiddenUntilExpanded(t *testing.T) {
	f := newReviewFixture(t, workflow.TargetInspection{Key: "watch.socket", Label: "watch.socket · Advanced user service", Current: workflow.TargetPresent, Desired: workflow.TargetUnknown, CaptureEligible: true, RequiresSelection: true, Advanced: true, Fingerprint: "socket-a"})
	if err := f.session.SetProviders([]workflow.Provider{reviewProvider{id: "services", targets: &f.targets, captured: &f.captured}}); err != nil {
		t.Fatal(err)
	}
	f.screen.statuses = []workflow.ProviderStatus{{ID: "services"}}
	f.openReview(t)
	if view := f.screen.View(); strings.Contains(view, "watch.socket") || !strings.Contains(view, "advanced hidden") {
		t.Fatalf("advanced candidate appeared before expansion:\n%s", view)
	}
	f.press(t, tea.KeyPressMsg{Code: 'v'})
	if view := f.screen.View(); !strings.Contains(view, "watch.socket") || !strings.Contains(view, "Not selected") {
		t.Fatalf("advanced candidate did not become reviewable:\n%s", view)
	}
}
