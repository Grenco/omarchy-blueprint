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
	if !strings.Contains(screen.View(), "Restore [Normal]") {
		t.Fatal("normal mode is not initial")
	}
	screen.Update(tea.KeyPressMsg{Code: 'f'})
	view := screen.View()
	if !strings.Contains(view, "Restore [Forced]") || !strings.Contains(view, "replace:1") || !strings.Contains(view, " *") {
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
	if view := screen.View(); !strings.Contains(view, "> config target") || !strings.Contains(view, "Restore [Normal]") || !strings.Contains(view, "create:1") {
		t.Fatalf("normal semantic markers missing: %q", view)
	}
	screen.Update(tea.KeyPressMsg{Code: 'f'})
	if view := screen.View(); !strings.Contains(view, "Restore [Forced]") || !strings.Contains(view, "delete:1") || !strings.Contains(view, "delete *") {
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
