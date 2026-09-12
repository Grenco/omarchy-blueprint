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
