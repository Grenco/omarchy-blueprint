package screens

import (
	tea "charm.land/bubbletea/v2"
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
	screen.targets = []workflow.TargetInspection{{Key: "backup.service", Label: "backup.service", Description: "Custom user service; review before managing", Recommended: true, CaptureEligible: true, RequiresSelection: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown}}
	if view := screen.View(); !strings.Contains(view, "backup.service") || strings.Contains(view, "review before managing") {
		t.Fatalf("uncaptured Services candidates were hidden:\n%s", view)
	}
}

func TestServicesInventoryGroupsOthersCollapsedAndExplainsInDetails(t *testing.T) {
	screen := NewProvider(nil, "services")
	screen.SetSize(100, 24)
	screen.targets = []workflow.TargetInspection{
		{Key: "dbus.service", Label: "dbus.service", Description: "External service", Desired: workflow.TargetUnknown},
		{Key: "backup.service", Label: "backup.service", Description: "Custom user service; review before managing", Recommended: true, Desired: workflow.TargetUnknown},
		{Key: "agent.service", Label: "agent.service", Description: "Managed customization", Desired: workflow.TargetPresent, Advanced: true},
	}
	view := screen.View()
	if !strings.Contains(view, "Custom & managed services") || !strings.Contains(view, "Other detected services") || strings.Contains(view, "dbus.service") || !strings.Contains(view, "backup.service") || !strings.Contains(view, "agent.service") {
		t.Fatalf("Services grouping failed:\n%s", view)
	}
	screen.list.Selected = 1
	if detail := screen.DetailView(); !strings.Contains(detail, "Why: Custom user service; review before managing") {
		t.Fatalf("discovery explanation missing: %s", detail)
	}
	screen.list.Selected, screen.selected = 3, 3
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := screen.View(); !strings.Contains(view, "dbus.service") {
		t.Fatalf("Enter did not expand other services:\n%s", view)
	}
}
