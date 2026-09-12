package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestOverviewScreenFlattensExpandedSectionsAndRoutesDecision(t *testing.T) {
	screen := &Overview{data: workflow.Overview{Items: []workflow.AttentionItem{{Severity: workflow.AttentionDecision, Provider: "config", Summary: "choose config", Target: "config", Ref: ".config/nvim"}, {Severity: workflow.AttentionDrift, Summary: "package differs"}, {Severity: workflow.AttentionInfo, Provider: "profile-git", Summary: "profile changes"}}, Healthy: []string{"themes"}}}
	for _, want := range []string{"[-] Needs review", "[-] Drift", "[-] Profile sync", "[-] Healthy", "Config: choose config", "✓ themes"} {
		if !strings.Contains(screen.View(), want) {
			t.Fatalf("view missing %q:\n%s", want, screen.View())
		}
	}
	// The first row is the section header; the next row is the first decision.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	message := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})().(OverviewTarget)
	if message.Target != "config" || message.Ref != ".config/nvim" {
		t.Fatalf("target=%#v", message)
	}
}

func TestOverviewSectionCollapseAndViewportKeepCursorVisible(t *testing.T) {
	items := make([]workflow.AttentionItem, 6)
	for i := range items {
		items[i] = workflow.AttentionItem{Severity: workflow.AttentionDecision, Summary: "decision " + string(rune('a'+i))}
	}
	screen := &Overview{height: 3, data: workflow.Overview{Items: items}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := screen.View(); !strings.Contains(view, "[+] Needs review") || strings.Contains(view, "decision a") {
		t.Fatalf("collapsed view=%q", view)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for range items {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	view := screen.View()
	if !strings.Contains(view, "> decision f") || strings.Contains(view, "decision a") {
		t.Fatalf("cursor or viewport wrong: %q", view)
	}
}
