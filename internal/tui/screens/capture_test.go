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
}
