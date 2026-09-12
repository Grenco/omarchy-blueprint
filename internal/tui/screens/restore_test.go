package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestRestoreScreenToggleUsesCachedComparison(t *testing.T) {
	screen := NewRestore(nil)
	screen.comparison = workflow.RestoreComparison{
		Normal:       model.RestorePlan{Operations: []model.Operation{{ID: "safe", Provider: "config", Resource: "safe", File: &model.FileWrite{ExpectedMissing: true}}}},
		Forced:       model.RestorePlan{Operations: []model.Operation{{ID: "replace", Provider: "config", Resource: "safe", File: &model.FileWrite{ReplaceExisting: true}}}},
		Consequences: []workflow.Consequence{{Provider: "config", Resource: "safe", Normal: workflow.OutcomeCreate, Forced: workflow.OutcomeReplace}},
	}
	if !strings.Contains(screen.View(), "active: [Normal]") || !strings.Contains(screen.View(), "NORMAL   FORCED") {
		t.Fatal("normal mode is not initial")
	}
	screen.Update(tea.KeyPressMsg{Code: 'f'})
	view := screen.View()
	if !strings.Contains(view, "active: [Forced]") || !strings.Contains(view, "replace:1") || !strings.Contains(view, "create   replace") {
		t.Fatalf("forced cached view missing expected state: %q", view)
	}
}

func TestRestoreScreenNoColorSemanticMarkers(t *testing.T) {
	screen := NewRestore(nil)
	screen.comparison = workflow.RestoreComparison{
		Normal:       model.RestorePlan{Operations: []model.Operation{{Provider: "config", Resource: "target", File: &model.FileWrite{ExpectedMissing: true}}}},
		Forced:       model.RestorePlan{Operations: []model.Operation{{Provider: "config", Resource: "target", Delete: &model.FileDelete{}}}},
		Consequences: []workflow.Consequence{{Provider: "config", Resource: "target", Normal: workflow.OutcomeCreate, Forced: workflow.OutcomeDelete}},
	}
	if view := screen.View(); !strings.Contains(view, "> config") || !strings.Contains(view, "active: [Normal]") || !strings.Contains(view, "create:1") {
		t.Fatalf("normal semantic markers missing: %q", view)
	}
	screen.Update(tea.KeyPressMsg{Code: 'f'})
	if view := screen.View(); !strings.Contains(view, "active: [Forced]") || !strings.Contains(view, "delete:1") || !strings.Contains(view, "create   delete") {
		t.Fatalf("forced semantic markers missing: %q", view)
	}
}

func TestRestoreScreenMetadataDetailDoesNotRenderBytes(t *testing.T) {
	screen := NewRestore(nil)
	screen.comparison.Consequences = []workflow.Consequence{{Provider: "config", Resource: "secret", Diff: &inspection.DiffDocument{Kind: inspection.DiffSensitive, Metadata: []inspection.DiffFact{{Key: "reason", Value: "credential"}}}}}
	screen.Update(tea.KeyPressMsg{Code: 'd'})
	if screen.diff == nil || !strings.Contains(screen.View(), "sensitive diff") {
		t.Fatalf("metadata detail was not opened safely: %q", screen.View())
	}
}

func TestRestoreScreenConfirmationDispatchesOneMutation(t *testing.T) {
	screen := NewRestore(nil)
	screen.comparison.Normal.Operations = []model.Operation{{ID: "write", File: &model.FileWrite{ExpectedMissing: true}}}
	modal := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})()
	if modal == nil {
		t.Fatal("restore did not request shared confirmation modal")
	}
	first := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	second := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if first == nil || second != nil || !screen.busy || screen.confirm {
		t.Fatalf("restore confirmation dispatched more than once: first=%v second=%v busy=%v confirm=%v", first != nil, second != nil, screen.busy, screen.confirm)
	}
}

func TestRestoreScreenComparisonTableAndDetailFollowSelection(t *testing.T) {
	screen := NewRestore(nil)
	screen.height = 10
	screen.comparison.Consequences = []workflow.Consequence{{Provider: "config", Resource: "one", Normal: workflow.OutcomeSkip, Forced: workflow.OutcomeReplace, Risk: model.RiskHigh, Difference: "force replaces one"}, {Provider: "config", Resource: "two", Normal: workflow.OutcomeCreate, Forced: workflow.OutcomeCreate, Difference: "creates two"}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if view := screen.View(); !strings.Contains(view, "skip     replace") || !strings.Contains(view, "HIGH") {
		t.Fatalf("simultaneous comparison or risk missing: %q", view)
	}
	if detail := screen.DetailView(); !strings.Contains(detail, "Resource: two") || !strings.Contains(detail, "Normal: create") {
		t.Fatalf("detail did not follow selected consequence: %q", detail)
	}
}

func TestRestoreScreenShowsAllCapturedScopeAndSemanticDifference(t *testing.T) {
	screen := NewRestore(nil)
	screen.comparison = workflow.RestoreComparison{
		Normal:       model.RestorePlan{Operations: []model.Operation{{Provider: "config", Resource: "target", File: &model.FileWrite{ExpectedMissing: true}}}},
		Forced:       model.RestorePlan{Operations: []model.Operation{{Provider: "config", Resource: "target", File: &model.FileWrite{ReplaceExisting: true}}}},
		Consequences: []workflow.Consequence{{Provider: "config", Resource: "target", Normal: workflow.OutcomeCreate, Forced: workflow.OutcomeReplace}},
	}
	view := screen.View()
	if !strings.Contains(view, "Scope: All captured providers") || !strings.Contains(view, "[diff]") || !strings.Contains(view, "Active normal plan") {
		t.Fatalf("restore scope or semantic outcome missing: %q", view)
	}
	if confirmation := screen.confirmation(); !strings.Contains(confirmation, "Restore all captured providers (normal mode)") {
		t.Fatalf("confirmation=%q", confirmation)
	}
}
