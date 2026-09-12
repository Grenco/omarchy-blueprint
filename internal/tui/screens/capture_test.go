package screens

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
	screen := &Capture{busy: true, capturing: true}
	cmd := screen.Update(captureDoneMsg{providers: []string{"packages", "themes"}})
	complete, ok := cmd().(CaptureComplete)
	if !ok || len(complete.Providers) != 2 || complete.Providers[0] != "packages" || complete.Providers[1] != "themes" || screen.busy || screen.capturing {
		t.Fatalf("completion=%#v busy=%v capturing=%v", complete, screen.busy, screen.capturing)
	}
}
