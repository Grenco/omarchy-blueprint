package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestProviderDefaultsToSavedTypedDataAndSwitchesToChanges(t *testing.T) {
	screen := &Provider{id: "packages", status: workflow.ProviderStatus{ID: "packages", Captured: true, Changes: []model.Change{{Summary: "git differs"}}, Snapshot: profile.Packages{Official: []string{"git"}, AUR: []string{"yay"}, Mise: profile.MiseTools{"node": {}}, Excluded: []string{"linux"}}}}
	view := screen.View()
	for _, want := range []string{"[Changes 1] [Saved *]", "Official packages", "git", "AUR packages", "yay", "Mise tools", "node", "Excluded packages", "linux"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "git differs") || strings.Contains(view, "{") {
		t.Fatalf("saved tab exposed drift or JSON: %q", view)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if view = screen.View(); !strings.Contains(view, "git differs") || strings.Contains(view, "Official packages") {
		t.Fatalf("changes tab=%q", view)
	}
}

func TestProviderLongChangesKeepSelectedRowVisible(t *testing.T) {
	changes := make([]model.Change, 6)
	for i := range changes {
		changes[i] = model.Change{Summary: "change " + string(rune('a'+i))}
	}
	screen := &Provider{height: 3, tab: "Changes", status: workflow.ProviderStatus{Captured: true, Changes: changes}}
	for range changes {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	view := screen.View()
	if !strings.Contains(view, "> change f") || strings.Contains(view, "change a") {
		t.Fatalf("viewport wrong: %q", view)
	}
}

func TestProviderSnapshotsRenderTypedSummariesNotJSON(t *testing.T) {
	for _, snapshot := range []any{profile.Packages{Official: []string{"git"}}, profile.Themes{Current: "nord"}, profile.Plugins{Items: []profile.Plugin{{ID: "clock"}}}, profile.Defaults{Terminal: "foot"}, profile.Shell{Version: 1, Hash: "hash"}, profile.Hooks{Items: []profile.Hook{{Path: "hook"}}}} {
		view := (&Provider{tab: "Saved", status: workflow.ProviderStatus{Captured: true, Snapshot: snapshot}}).View()
		if strings.Contains(view, "{") || strings.Contains(view, "\"") {
			t.Fatalf("snapshot rendered as JSON: %q", view)
		}
	}
}
