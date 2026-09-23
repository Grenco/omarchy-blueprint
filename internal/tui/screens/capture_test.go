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

func TestCaptureFreshProfileAddsGuidanceWithoutHidingCategories(t *testing.T) {
	screen := &Capture{
		width: 80,
		statuses: []workflow.ProviderStatus{
			{ID: "packages"},
			{ID: "themes"},
			{ID: "hooks"},
		},
		chosen: map[string]bool{},
	}

	view := screen.View()
	for _, want := range []string{
		"Choose what this profile should remember",
		"you do not need to capture everything",
		"CATEGORY",
		"packages",
		"themes",
		"hooks",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("fresh Capture missing %q:\n%s", want, view)
		}
	}
}

func TestCaptureFreshProfileGuidanceDisappearsOnceAnyCategoryIsCaptured(t *testing.T) {
	screen := &Capture{
		statuses: []workflow.ProviderStatus{
			{ID: "packages", Captured: true},
			{ID: "themes"},
		},
		chosen: map[string]bool{},
	}

	if view := screen.View(); strings.Contains(view, "Choose what this profile should remember") {
		t.Fatalf("fresh hint remained after capture:\n%s", view)
	}
}

func freshCaptureCategories() []workflow.ProviderStatus {
	ids := []string{"packages", "themes", "plugins", "config", "defaults", "shell", "hooks", "resources"}
	statuses := make([]workflow.ProviderStatus, len(ids))
	for i, id := range ids {
		statuses[i] = workflow.ProviderStatus{ID: id}
	}
	return statuses
}

// TestCaptureConstrainedHeightKeepsSelectedCategoryVisible reproduces the
// supported 80x18 terminal, where Capture receives roughly a 78x10 content
// budget once header/footer/description overhead is subtracted. The table
// must scroll to keep the cursor visible instead of the outer panel
// silently clipping rows that are still "rendered" past its height.
func TestCaptureConstrainedHeightKeepsSelectedCategoryVisible(t *testing.T) {
	statuses := freshCaptureCategories()
	screen := &Capture{statuses: statuses, chosen: map[string]bool{}}
	screen.SetSize(78, 10) // the real width/height Capture receives at 80x18.

	for i := 0; i < len(statuses)-1; i++ {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if screen.cursor != len(statuses)-1 {
		t.Fatalf("cursor = %d, want %d", screen.cursor, len(statuses)-1)
	}

	view := screen.View()
	lines := strings.Split(view, "\n")
	// The outer workspace panel bounds its content to the height Capture was
	// granted; a taller returned string means the panel (not Capture) decides
	// which lines survive, which is how the last category silently vanished.
	if len(lines) > 10 {
		t.Fatalf("Capture.View() returned %d lines for a 10-line budget; the outer panel would clip it instead of Capture scrolling itself:\n%s", len(lines), view)
	}
	if !strings.Contains(view, statuses[len(statuses)-1].ID) {
		t.Fatalf("final selected category %q not visible at 80x18-equivalent size:\n%s", statuses[len(statuses)-1].ID, view)
	}
	if !strings.Contains(view, "Choose what this profile should remember") {
		t.Fatalf("onboarding guidance disappeared even though space permits it:\n%s", view)
	}
}

func TestCaptureConstrainedHeightAtWiderWidthsAlsoKeepsSelectionVisible(t *testing.T) {
	statuses := freshCaptureCategories()
	for _, size := range []struct{ width, height int }{{98, 15}, {150, 20}} {
		screen := &Capture{statuses: statuses, chosen: map[string]bool{}}
		screen.SetSize(size.width, size.height)
		for i := 0; i < len(statuses)-1; i++ {
			screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		view := screen.View()
		if !strings.Contains(view, statuses[len(statuses)-1].ID) {
			t.Fatalf("%dx%d: final selected category not visible:\n%s", size.width, size.height, view)
		}
		if !strings.Contains(view, "Choose what this profile should remember") {
			t.Fatalf("%dx%d: onboarding guidance missing:\n%s", size.width, size.height, view)
		}
	}
}

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
	if view := screen.View(); !strings.Contains(view, "CATEGORY") || strings.Contains(view, "PROVIDER") {
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
