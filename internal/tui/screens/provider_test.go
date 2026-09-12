package screens

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
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

func TestProviderScreenShowsCaptureForUncapturedState(t *testing.T) {
	screen := &Provider{id: "hooks"}
	if view := screen.View(); !strings.Contains(view, "Not captured. Press c to capture.") {
		t.Fatalf("view=%q", view)
	}
}
