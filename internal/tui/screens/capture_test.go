package screens

import (
	"errors"
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
	if !ok || request.Title != "Capture selected categories" || !screen.chosen["packages"] || !screen.chosen["themes"] || !screen.captureAll {
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

func TestCaptureCleanupWarningStillCompletes(t *testing.T) {
	screen := &Capture{chosen: map[string]bool{"packages": true}}
	cmd := screen.Update(captureDoneMsg{providers: []string{"packages"}, warning: errors.New("backup cleanup")})
	complete := cmd().(CaptureComplete)
	if complete.Err != nil || complete.Warning == nil || len(complete.Providers) != 1 || len(screen.chosen) != 0 {
		t.Fatalf("complete=%#v chosen=%#v", complete, screen.chosen)
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

func TestCaptureUsesCategoryTerminology(t *testing.T) {
	screen := &Capture{statuses: []workflow.ProviderStatus{{ID: "themes"}}, chosen: map[string]bool{}}
	if view := screen.View(); !strings.Contains(view, "Category") || strings.Contains(view, "Provider") {
		t.Fatalf("capture terminology=%q", view)
	}
	if detail := (&Capture{chosen: map[string]bool{}}).DetailView(); detail != "Select categories to capture." {
		t.Fatalf("empty detail=%q", detail)
	}
}

func TestCaptureSanitizesProviderValuesAndErrors(t *testing.T) {
	screen := &Capture{statuses: []workflow.ProviderStatus{{ID: "bad\nprovider\x1b"}}, chosen: map[string]bool{}}
	if view := screen.View(); strings.Contains(view, "\x1b") || !strings.Contains(view, "bad?provider?") {
		t.Fatalf("unsafe capture=%q", view)
	}
	screen.err = errors.New("bad\nerror\x1b")
	if view := screen.View(); strings.Contains(view, "\x1b") || !strings.Contains(view, "bad?error?") {
		t.Fatalf("unsafe error=%q", view)
	}
}

func TestCaptureScreenIgnoresStaleStatus(t *testing.T) {
	screen := &Capture{requestID: 2, busy: true}
	screen.Update(captureStatusMsg{requestID: 1, statuses: []workflow.ProviderStatus{{ID: "stale"}}})
	if !screen.busy || len(screen.statuses) != 0 {
		t.Fatalf("stale status changed state: busy=%v statuses=%#v", screen.busy, screen.statuses)
	}
}
