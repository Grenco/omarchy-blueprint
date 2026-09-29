package screens

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestServicesSavedStateUsesExistingProviderTable(t *testing.T) {
	screen := NewProvider(nil, "services")
	screen.SetSize(80, 20)
	screen.status = workflow.ProviderStatus{ID: "services", Captured: true, Snapshot: profile.Services{Units: []profile.ServiceUnit{{Name: "backup.timer", Presence: profile.ServicePresent, StartIntent: profile.ServiceStartEnabled, Management: profile.ServiceManagementDefinition}}}}
	view := screen.View()
	if !strings.Contains(view, "backup.timer") || !strings.Contains(view, "Starts automatically") {
		t.Fatalf("saved Services intent is missing from its basic TUI inventory:\n%s", view)
	}
}

func TestServicesUncapturedScreenShowsReadOnlyCandidates(t *testing.T) {
	screen := NewProvider(nil, "services")
	screen.SetSize(80, 20)
	screen.status = workflow.ProviderStatus{ID: "services", Captured: false}
	screen.targets = []workflow.TargetInspection{{Key: "backup.service", Label: "backup.service · Custom user service", CaptureEligible: true, RequiresSelection: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown}}
	if view := screen.View(); !strings.Contains(view, "backup.service") || !strings.Contains(view, "Custom user service") {
		t.Fatalf("uncaptured Services candidates were hidden:\n%s", view)
	}
}
