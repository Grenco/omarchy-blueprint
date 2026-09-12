package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestOverviewScreenSectionsAndNavigationTarget(t *testing.T) {
	screen := &Overview{data: workflow.Overview{Items: []workflow.AttentionItem{{Severity: workflow.AttentionDecision, Summary: "choose config", Target: "config", Ref: ".config/nvim"}, {Severity: workflow.AttentionDrift, Summary: "package differs"}, {Severity: workflow.AttentionInfo, Summary: "profile changes"}}, Healthy: []string{"themes"}}}
	for _, want := range []string{"Needs review", "Drift", "Profile sync", "Healthy", "choose config", "themes"} {
		if !strings.Contains(screen.View(), want) {
			t.Fatalf("view missing %q:\n%s", want, screen.View())
		}
	}
	cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	message := cmd().(OverviewTarget)
	if message.Target != "config" || message.Ref != ".config/nvim" {
		t.Fatalf("target=%#v", message)
	}
}

func TestOverviewSelectionFollowsDisplayedSectionOrder(t *testing.T) {
	screen := &Overview{data: workflow.Overview{Items: []workflow.AttentionItem{
		{Severity: workflow.AttentionDrift, Summary: "drift", Target: "config", Ref: "drift"},
		{Severity: workflow.AttentionDecision, Summary: "review", Target: "resources", Ref: "review"},
		{Severity: workflow.AttentionInfo, Provider: "profile-git", Summary: "sync", Target: "sync", Ref: "sync"},
	}}}
	if view := screen.View(); strings.Index(view, "review") > strings.Index(view, "drift") {
		t.Fatalf("sections are not displayed in priority order: %q", view)
	}
	screen.Update(tea.KeyPressMsg{Code: 'j'})
	message := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})().(OverviewTarget)
	if message.Ref != "drift" {
		t.Fatalf("selected item followed source order instead of display order: %#v", message)
	}
}
