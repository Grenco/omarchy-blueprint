package screens

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// workspaceSizes are the Config/Sync/Restore workspace interiors the
// application produces at 70×18, 80×24, 100×30 and 120×40 (sidebar,
// details pane, borders and the screen description taken out), plus a
// narrow and a tall case.
var workspaceSizes = []struct{ width, height int }{
	{68, 9}, {58, 15}, {76, 21}, {94, 31}, {40, 12}, {120, 40},
}

// assertFits fails when a screen's view is taller or wider than its
// workspace: screens must budget their own content rather than rely on the
// panel to clip it, or scrolling runs beneath the border.
func assertFits(t *testing.T, name, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Fatalf("%s emitted %d lines for a %dx%d workspace:\n%s", name, len(lines), width, height, view)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("%s line is %d columns in a %dx%d workspace: %q", name, w, width, height, line)
		}
	}
}
