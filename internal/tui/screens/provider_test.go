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
	for _, want := range []string{"Changes 1   [active] Saved", "Official packages", "git", "AUR packages", "yay", "Mise tools", "node"} {
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

func TestProviderCollapsesSavedGroup(t *testing.T) {
	screen := &Provider{id: "packages", status: workflow.ProviderStatus{Captured: true, Snapshot: profile.Packages{Official: []string{"git", "curl"}}}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := screen.View()
	if !strings.Contains(view, "Official packages") || strings.Contains(view, "git") || strings.Contains(view, "curl") {
		t.Fatalf("collapsed view=%q", view)
	}
}

func TestProviderGroupsIncludedAndExcludedItemsWithStateMarkers(t *testing.T) {
	screen := &Provider{id: "packages", status: workflow.ProviderStatus{Captured: true, Snapshot: profile.Packages{Official: []string{"git"}, Excluded: []string{"official:1password"}}}}
	view := screen.View()
	if !strings.Contains(view, "+ git") || !strings.Contains(view, "- 1password") || strings.Contains(view, "[included]") || strings.Contains(view, "[not included]") {
		t.Fatalf("state markers=%q", view)
	}
	if strings.Index(view, "+ git") > strings.Index(view, "- 1password") {
		t.Fatalf("excluded item rendered before included item: %q", view)
	}
}

func TestProviderGroupsPluginsBySource(t *testing.T) {
	snapshot := profile.Plugins{Items: []profile.Plugin{{ID: "clock", Source: "builtin", Enabled: true}, {ID: "repo", Source: "git", Enabled: true}, {ID: "private", Source: "local"}}, Excluded: []string{"private"}}
	view := (&Provider{id: "plugins", status: workflow.ProviderStatus{Captured: true, Snapshot: snapshot}}).View()
	for _, want := range []string{"Built-in plugins", "+ clock", "Git plugins", "+ repo", "Local plugins", "- private"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}
