package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The screen tests check their views against these workspaces; keep them
// honest by pinning what the application actually gives each screen.
func TestScreenWorkspacesAtSupportedTerminalSizes(t *testing.T) {
	want := map[[2]int]map[ScreenID][2]int{
		{70, 18}:  {ScreenConfig: {68, 9}, ScreenSync: {68, 10}, ScreenRestore: {68, 9}, ScreenCapture: {68, 10}, ScreenResources: {68, 9}},
		{80, 24}:  {ScreenConfig: {78, 15}, ScreenSync: {78, 16}, ScreenRestore: {78, 16}, ScreenCapture: {78, 16}, ScreenResources: {78, 16}},
		{100, 30}: {ScreenConfig: {75, 21}, ScreenSync: {75, 22}, ScreenRestore: {75, 21}, ScreenCapture: {75, 22}, ScreenResources: {75, 22}},
		{120, 40}: {ScreenConfig: {64, 31}, ScreenSync: {64, 31}, ScreenRestore: {64, 31}, ScreenCapture: {64, 32}, ScreenResources: {64, 31}},
	}
	for terminal, screens := range want {
		m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: terminal[0], Height: terminal[1]})
		for id, size := range screens {
			if w, h := m.screenSize(id); w != size[0] || h != size[1] {
				t.Errorf("%dx%d %s workspace = %dx%d, want %dx%d (update the screen tests' workspaceSizes)", terminal[0], terminal[1], id, w, h, size[0], size[1])
			}
		}
	}
}
