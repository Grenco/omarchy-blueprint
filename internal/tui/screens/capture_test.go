package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestCaptureAllRequestsConfirmation(t *testing.T) {
	screen := &Capture{statuses: []workflow.ProviderStatus{{ID: "packages"}, {ID: "themes"}}, chosen: map[string]bool{}}
	cmd := screen.Update(tea.KeyPressMsg{Code: 'C'})
	request, ok := cmd().(components.ModalRequest)
	if !ok || request.Title != "Capture selected providers" || !screen.chosen["packages"] || !screen.chosen["themes"] || !screen.captureAll {
		t.Fatalf("request=%#v chosen=%#v", request, screen.chosen)
	}
	if view := screen.View(); view == "" || view == "Capture selected providers?\n\nEnter confirms. Escape cancels." {
		t.Fatalf("confirmation replaced Capture background: %q", view)
	}
}

func TestCaptureCompletionIncludesAffectedProviders(t *testing.T) {
	screen := &Capture{busy: true, capturing: true, chosen: map[string]bool{"packages": true}}
	cmd := screen.Update(captureDoneMsg{providers: []string{"packages", "themes"}})
	complete, ok := cmd().(CaptureComplete)
	if !ok || len(complete.Providers) != 2 || complete.Providers[0] != "packages" || complete.Providers[1] != "themes" || len(screen.chosen) != 0 || screen.busy || screen.capturing {
		t.Fatalf("completion=%#v busy=%v capturing=%v", complete, screen.busy, screen.capturing)
	}
}

func TestCaptureSelectChangedReplacesExistingSelection(t *testing.T) {
	screen := &Capture{statuses: []workflow.ProviderStatus{{ID: "packages", Captured: true, Changes: []model.Change{{}}}, {ID: "themes", Captured: true}, {ID: "plugins", Captured: true, Changes: []model.Change{{}}}}, chosen: map[string]bool{"themes": true}}
	screen.Update(tea.KeyPressMsg{Code: 'a'})
	if !screen.chosen["packages"] || !screen.chosen["plugins"] || screen.chosen["themes"] || len(screen.chosen) != 2 {
		t.Fatalf("chosen=%#v", screen.chosen)
	}
}

func TestCaptureViewLabelsUncapturedProviders(t *testing.T) {
	screen := &Capture{statuses: []workflow.ProviderStatus{{ID: "themes"}}, chosen: map[string]bool{}}
	if view := screen.View(); !strings.Contains(view, "not captured") {
		t.Fatalf("view does not label uncaptured provider: %q", view)
	}
}

func TestCaptureScreenIgnoresStaleStatus(t *testing.T) {
	screen := &Capture{requestID: 2, busy: true}
	screen.Update(captureStatusMsg{requestID: 1, statuses: []workflow.ProviderStatus{{ID: "stale"}}})
	if !screen.busy || len(screen.statuses) != 0 {
		t.Fatalf("stale status changed state: busy=%v statuses=%#v", screen.busy, screen.statuses)
	}
}
