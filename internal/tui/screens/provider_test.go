package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestProviderScreenRendersSemanticStatus(t *testing.T) {
	screen := &Provider{id: "themes", status: workflow.ProviderStatus{ID: "themes", Captured: true, Changes: []model.Change{{Summary: "~ theme nord differs"}}}}
	view := screen.View()
	for _, want := range []string{"Themes", "~ theme nord differs", "c capture"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestProviderScreenDownSelectsNextChange(t *testing.T) {
	screen := &Provider{status: workflow.ProviderStatus{Changes: []model.Change{{Summary: "first"}, {Summary: "second"}}}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if change := screen.selectedChange(); change.Summary != "second" {
		t.Fatalf("selected change=%#v", change)
	}
}

func TestProviderSnapshotsRenderTypedSummariesNotJSON(t *testing.T) {
	for _, snapshot := range []any{
		profile.Packages{Official: []string{"git"}}, profile.Themes{Current: "nord"}, profile.Plugins{Items: []profile.Plugin{{ID: "clock"}}},
		profile.Defaults{Terminal: "foot"}, profile.Shell{Version: 1, Hash: "hash"}, profile.Hooks{Items: []profile.Hook{{Path: "hook"}}},
	} {
		view := (&Provider{status: workflow.ProviderStatus{Captured: true, Snapshot: snapshot}}).View()
		if strings.Contains(view, "{") || strings.Contains(view, "\"") {
			t.Fatalf("snapshot rendered as JSON: %q", view)
		}
	}
}

func TestProviderScreenUsesTypedSnapshotDetails(t *testing.T) {
	screen := &Provider{id: "packages", status: workflow.ProviderStatus{ID: "packages", Captured: true, Snapshot: profile.Packages{Official: []string{"git"}, AUR: []string{"yay"}}}}
	detail := screen.DetailView()
	if !strings.Contains(detail, "official: 1  aur: 1") || strings.Contains(detail, "{") {
		t.Fatalf("detail=%q", detail)
	}
}

func TestProviderScreenShowsCaptureForUncapturedState(t *testing.T) {
	screen := &Provider{id: "hooks"}
	if view := screen.View(); !strings.Contains(view, "Not captured. Press c to capture.") {
		t.Fatalf("view=%q", view)
	}
}
