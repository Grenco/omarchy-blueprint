package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"github.com/charmbracelet/x/ansi"
)

func themeStyles(added, removed string) components.Styles {
	return components.NewStyles(components.ThemePalette{ColorEnabled: true, Added: added, Removed: removed, Green: added, Red: removed, Accent: "#88aaff", Muted: "#777777"})
}

// lineWith returns the rendered line containing text, styling intact.
func lineWith(t *testing.T, view, text string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(line), text) {
			return line
		}
	}
	t.Fatalf("no line contains %q:\n%s", text, view)
	return ""
}

// The theme's colours as 24-bit foreground sequences.
const greenFG, redFG, otherGreenFG = "38;2;0;170;0", "38;2;204;0;0", "38;2;0;255;0"

func assertColoured(t *testing.T, view, wantAdded, wantRemoved string) {
	t.Helper()
	if added := lineWith(t, view, "line 1"); !strings.Contains(added, wantAdded) {
		t.Fatalf("added line not in the theme's green (%s): %q", wantAdded, added)
	}
	if removed := lineWith(t, view, "line 0"); !strings.Contains(removed, wantRemoved) {
		t.Fatalf("removed line not in the theme's red (%s): %q", wantRemoved, removed)
	}
	if header := lineWith(t, view, "@@"); strings.Contains(header, wantAdded) || strings.Contains(header, wantRemoved) {
		t.Fatalf("context is coloured as a change: %q", header)
	}
}

func TestConfigDiffUsesTheThemeAndFollowsThemeChanges(t *testing.T) {
	document := longDiff(4)
	screen := &Config{width: 120, height: 30, candidates: manyConfigCandidates(1)}
	screen.SetStyles(themeStyles("#00aa00", "#cc0000"))
	screen.inspection = workflow.ConfigInspection{BaselineToLive: &document}
	screen.diff = &components.DiffViewer{}
	screen.inspectionID = 1
	screen.Update(configInspectionMsg{requestID: 1, inspection: workflow.ConfigInspection{Candidate: screen.selectedCandidate(), BaselineToLive: &document}})
	assertColoured(t, screen.View(), greenFG, redFG)
	screen.SetStyles(themeStyles("#00ff00", "#cc0000"))
	assertColoured(t, screen.View(), otherGreenFG, redFG)
}

func TestSyncDiffUsesTheThemeAndFollowsThemeChanges(t *testing.T) {
	screen := &Sync{width: 120, height: 30}
	screen.SetStyles(themeStyles("#00aa00", "#cc0000"))
	screen.requestID = 2
	screen.Update(syncDiffMsg{requestID: 2, diff: profilegit.Diff{Files: []profilegit.DiffFile{{Document: longDiff(4)}}}})
	if screen.diff == nil {
		t.Fatal("diff did not open")
	}
	assertColoured(t, screen.View(), greenFG, redFG)
	screen.SetStyles(themeStyles("#00ff00", "#cc0000"))
	assertColoured(t, screen.View(), otherGreenFG, redFG)
}

func TestDiffWithoutColourKeepsItsMarkersInBothLayouts(t *testing.T) {
	viewer := components.NewDiffViewer(longDiff(4))
	viewer.SetStyles(components.NewStyles(components.ThemePalette{ColorEnabled: false, Added: "#00aa00", Removed: "#cc0000"}))
	for _, width := range []int{80, 140} {
		viewer.SetSize(width, 20)
		if width >= 100 {
			viewer.Update(tea.KeyPressMsg{Code: 's', Text: "s"}) // side-by-side
		}
		view := viewer.View()
		if strings.Contains(view, "\x1b[") {
			t.Fatalf("NO_COLOR diff carries colour (width %d): %q", width, view)
		}
		added, removed := lineWith(t, view, "line 1"), lineWith(t, view, "line 0")
		if !strings.Contains(added, "+") || !strings.Contains(removed, "-") {
			t.Fatalf("width %d: added %q / removed %q lack their +/- markers", width, added, removed)
		}
	}
}

var _ = inspection.DiffText
