package screens

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestOverviewExplainsCompletelyUncapturedProfile(t *testing.T) {
	session, _ := newSyncSession(t)
	screen := NewOverview(session)

	view := screen.View()
	for _, want := range []string{
		"Nothing has been captured yet",
		"Capture is where you choose which parts of this machine Blueprint should remember.",
		"open Capture",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("fresh overview missing %q:\n%s", want, view)
		}
	}
}

func TestOverviewFreshProfileGuidanceDisappearsAfterCapture(t *testing.T) {
	session, _ := newSyncSession(t)
	if err := session.SetProviderCaptured(context.Background(), "hooks", true); err != nil {
		t.Fatal(err)
	}
	screen := NewOverview(session)

	if view := screen.View(); strings.Contains(view, "Nothing has been captured yet") {
		t.Fatalf("fresh-profile guidance remained after capture:\n%s", view)
	}
}

func TestOverviewScreenFlattensExpandedSectionsAndRoutesDecision(t *testing.T) {
	screen := &Overview{data: workflow.Overview{Items: []workflow.AttentionItem{{Severity: workflow.AttentionDecision, Provider: "config", Summary: "choose config", Target: "config", Ref: ".config/nvim"}, {Severity: workflow.AttentionDrift, Summary: "package differs"}, {Severity: workflow.AttentionInfo, Provider: "profile-git", Summary: "profile changes"}}, Healthy: []string{"themes"}}}
	for _, want := range []string{"▼ Needs review", "▼ Drift", "▼ Profile sync", "▼ Healthy", "Config: choose config", "✓ themes"} {
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

func TestOverviewSanitizesAttentionAndErrors(t *testing.T) {
	screen := &Overview{data: workflow.Overview{Items: []workflow.AttentionItem{{Severity: workflow.AttentionDecision, Summary: "bad\nsummary\x1b"}}}}
	if view := screen.View(); strings.Contains(view, "\x1b") || !strings.Contains(view, "bad?summary?") {
		t.Fatalf("unsafe overview=%q", view)
	}
	screen.err = errors.New("bad\nerror\x1b")
	if view := screen.View(); strings.Contains(view, "\x1b") || !strings.Contains(view, "bad?error?") {
		t.Fatalf("unsafe error=%q", view)
	}
}

func TestOverviewSectionCollapseAndViewportKeepCursorVisible(t *testing.T) {
	items := make([]workflow.AttentionItem, 6)
	for i := range items {
		items[i] = workflow.AttentionItem{Severity: workflow.AttentionDecision, Summary: "decision " + string(rune('a'+i))}
	}
	screen := &Overview{height: 3, data: workflow.Overview{Items: items}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := screen.View(); !strings.Contains(view, "▶ Needs review") || strings.Contains(view, "decision a") {
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

func TestOverviewGroupJumpsMoveBetweenSectionHeaders(t *testing.T) {
	screen := &Overview{data: workflow.Overview{Items: []workflow.AttentionItem{{Severity: workflow.AttentionDecision, Summary: "review"}, {Severity: workflow.AttentionDrift, Summary: "drift"}}, Healthy: []string{"themes"}}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	screen.Update(tea.KeyPressMsg{Code: '['})
	if screen.selected != 0 {
		t.Fatalf("previous group selected=%d", screen.selected)
	}
	screen.Update(tea.KeyPressMsg{Code: ']'})
	if screen.selected != 2 {
		t.Fatalf("next group selected=%d", screen.selected)
	}
}

func TestOverviewIgnoresStaleRefresh(t *testing.T) {
	screen := &Overview{requestID: 2, busy: true}
	screen.Update(overviewMsg{requestID: 1, data: workflow.Overview{Healthy: []string{"stale"}}})
	if !screen.busy || len(screen.data.Healthy) != 0 {
		t.Fatalf("stale refresh changed state: busy=%v data=%#v", screen.busy, screen.data)
	}
}
