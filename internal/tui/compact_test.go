package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
)

const (
	navigationTitle = "┌ Navigation "
	detailsTitle    = "┌ Details "
)

// assertFocusIsVisible enforces the compact invariant: root focus may only
// name a pane that is actually rendered, and a rendered overlay always owns
// focus.
func assertFocusIsVisible(t *testing.T, m model) {
	t.Helper()
	view := m.View().Content
	nav, details := strings.Contains(view, navigationTitle), strings.Contains(view, detailsTitle)
	if layoutForSize(m.width, m.height).mode != LayoutCompact {
		if m.focus == focusSidebar && !nav {
			t.Fatalf("sidebar focus without a visible Navigation pane:\n%s", view)
		}
		if m.focus == focusDetails && !details {
			t.Fatalf("details focus without a visible Details pane:\n%s", view)
		}
		return
	}
	switch m.focus {
	case focusSidebar:
		if !nav || details {
			t.Fatalf("compact sidebar focus must render only the Navigation overlay (nav=%v details=%v):\n%s", nav, details, view)
		}
	case focusDetails:
		if !details || nav {
			t.Fatalf("compact details focus must render only the Details overlay (nav=%v details=%v):\n%s", nav, details, view)
		}
	default:
		if nav || details {
			t.Fatalf("compact workspace focus must not render an overlay (nav=%v details=%v):\n%s", nav, details, view)
		}
	}
}

func compactModel(t *testing.T) model {
	t.Helper()
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 80, Height: 24})
	assertFocusIsVisible(t, m)
	return m
}

func press(t *testing.T, m model, key tea.KeyPressMsg) model {
	t.Helper()
	m = updateModel(t, m, key)
	assertFocusIsVisible(t, m)
	return m
}

var (
	keyLeft  = tea.KeyPressMsg{Code: tea.KeyLeft}
	keyRight = tea.KeyPressMsg{Code: tea.KeyRight}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEsc}
	keyTab   = tea.KeyPressMsg{Code: tea.KeyTab}
)

func TestCompactTabbedScreensKeepTabForLocalTabsAndUseArrowsForNavigation(t *testing.T) {
	for _, test := range []struct {
		name   string
		id     ScreenID
		screen screen
	}{
		{name: "Config", id: ScreenConfig, screen: &configScreen{Config: screens.NewConfig(nil)}},
		{name: "Packages", id: ScreenPackages, screen: &providerScreen{Provider: screens.NewProvider(nil, "packages"), id: ScreenPackages}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := compactModel(t)
			m.screens[test.id] = test.screen
			m.selected = screenIndex(t, test.id)
			m.setScreenSizes()

			for _, want := range []string{"Capture", "Restore", "State"} {
				m = press(t, m, keyTab)
				if view := m.activeScreen().View(); !strings.Contains(view, "[active] "+want) {
					t.Fatalf("Tab did not move to %s: %q", want, view)
				}
				if m.focus != focusWorkspace {
					t.Fatalf("Tab changed root focus to %d; local tabs own Tab", m.focus)
				}
			}

			m = press(t, m, keyLeft)
			if m.focus != focusSidebar {
				t.Fatalf("Left focus = %d, want visible Navigation", m.focus)
			}
			m = press(t, m, keyRight)
			if m.focus != focusWorkspace || m.screenID() != test.id {
				t.Fatalf("Right should close Navigation onto %s, got focus=%d screen=%s", test.id, m.focus, m.screenID())
			}
			m = press(t, m, keyLeft)
			m = press(t, m, keyEnter)
			if m.focus != focusWorkspace || m.screenID() != test.id {
				t.Fatalf("Enter should close Navigation onto %s, got focus=%d screen=%s", test.id, m.focus, m.screenID())
			}
		})
	}
}

func TestCompactUntabbedScreenUsesArrowsForNavigationAndTabDoesNotOpenIt(t *testing.T) {
	m := compactModel(t)
	if m.screenID() != ScreenOverview {
		t.Fatalf("start screen = %s", m.screenID())
	}
	m = press(t, m, keyTab)
	if m.focus != focusWorkspace {
		t.Fatalf("compact Tab moved focus to %d; Tab must not open Navigation", m.focus)
	}

	m = press(t, m, keyLeft)
	if m.focus != focusSidebar {
		t.Fatalf("Left focus = %d, want Navigation", m.focus)
	}
	m = press(t, m, tea.KeyPressMsg{Code: 'j'})
	if m.screenID() != ScreenCapture || m.focus != focusSidebar {
		t.Fatalf("j in Navigation: screen=%s focus=%d", m.screenID(), m.focus)
	}
	m = press(t, m, keyEnter)
	if m.focus != focusWorkspace || m.screenID() != ScreenCapture {
		t.Fatalf("Enter should open Capture in the workspace: focus=%d screen=%s", m.focus, m.screenID())
	}

	m = press(t, m, tea.KeyPressMsg{Code: 'h'})
	m = press(t, m, tea.KeyPressMsg{Code: 'l'})
	if m.focus != focusWorkspace {
		t.Fatalf("h then l should round-trip to the workspace, focus=%d", m.focus)
	}
	m = press(t, m, keyLeft)
	m = press(t, m, keyEsc)
	if m.focus != focusWorkspace {
		t.Fatalf("Esc should close Navigation, focus=%d", m.focus)
	}
	m = press(t, m, keyLeft)
	m = press(t, m, keyLeft)
	if m.focus != focusSidebar {
		t.Fatalf("Left at the Navigation edge should stay put, focus=%d", m.focus)
	}
}

func TestCompactDetailsOpensAsVisibleOverlay(t *testing.T) {
	m := compactModel(t)
	if _, ok := m.activeScreen().(DetailView); !ok {
		t.Fatal("fixture requires a screen with DetailView")
	}
	m = press(t, m, keyRight)
	if m.focus != focusDetails {
		t.Fatalf("Right focus = %d, want visible Details", m.focus)
	}
	m = press(t, m, keyRight)
	if m.focus != focusDetails {
		t.Fatalf("Right at the Details edge should stay put, focus=%d", m.focus)
	}
	m = press(t, m, tea.KeyPressMsg{Code: 'j'})
	m = press(t, m, keyLeft)
	if m.focus != focusWorkspace {
		t.Fatalf("Left should close Details, focus=%d", m.focus)
	}
	m = press(t, m, keyRight)
	m = press(t, m, keyEsc)
	if m.focus != focusWorkspace {
		t.Fatalf("Esc should close Details, focus=%d", m.focus)
	}
}

type noDetailScreen struct{ id ScreenID }

func (s *noDetailScreen) ID() ScreenID           { return s.id }
func (s *noDetailScreen) SetSize(int, int)       {}
func (s *noDetailScreen) Update(tea.Msg) tea.Cmd { return nil }
func (s *noDetailScreen) View() string           { return "no details here" }
func (s *noDetailScreen) Actions() []Action      { return nil }

func TestPanesWithoutDetailsNeverTakeInvisibleDetailsFocus(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 100, Height: 30}, {Width: 140, Height: 40}} {
		m := updateModel(t, newModel(ThemeLoader{NoColor: true}), size)
		m.screens[ScreenSync] = &noDetailScreen{id: ScreenSync}
		m.selectScreen(ScreenSync)
		m = press(t, m, keyRight)
		m = press(t, m, keyRight)
		if m.focus == focusDetails {
			t.Fatalf("%dx%d: Right reached Details on a screen without DetailView", size.Width, size.Height)
		}
		m = press(t, m, keyTab)
		m = press(t, m, keyTab)
		if m.focus == focusDetails {
			t.Fatalf("%dx%d: Tab reached Details on a screen without DetailView", size.Width, size.Height)
		}
	}
}

func TestResizeNormalizesFocusAcrossLayouts(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 140, Height: 40})
	m.focus = focusDetails
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	assertFocusIsVisible(t, m)
	if m.focus != focusWorkspace {
		t.Fatalf("three-pane → compact should land on the workspace, focus=%d", m.focus)
	}

	m.focus = focusSidebar
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = press(t, m, keyLeft)
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	assertFocusIsVisible(t, m)

	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	assertFocusIsVisible(t, m)
	m = press(t, m, keyRight)
	m = press(t, m, keyRight)
	if m.focus != focusDetails {
		t.Fatalf("setup: expected compact Details, focus=%d", m.focus)
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	assertFocusIsVisible(t, m)
	if m.focus == focusDetails {
		t.Fatal("two-pane layout has no Details pane but kept Details focus")
	}
}

// The Restore viewport tests in package screens assume the workspace the root
// actually grants at 80x24. Pin that contract so the two cannot drift.
func TestRestoreWorkspaceBudgetAt80x24MatchesScreenTests(t *testing.T) {
	m := compactModel(t)
	width, height := m.screenSize(ScreenRestore)
	if width != 78 || height != 16 {
		t.Fatalf("Restore workspace at 80x24 = %dx%d; update restoreCompactWidth/Height in screens/restore_viewport_test.go", width, height)
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.selectScreen(ScreenRestore)
	layout := layoutForSize(m.width, m.height)
	content := strings.Split(m.workspaceContent(layout.workspaceWidth), "\n")
	if _, interior := components.InteriorSize(layout.workspaceWidth, layout.contentHeight); len(content) > interior {
		t.Fatalf("Restore workspace content is %d lines for a %d-line panel interior", len(content), interior)
	}
}

// busyScreen models a screen that is loading (TransientActive) and would
// swallow every key it is offered.
type busyScreen struct {
	id   ScreenID
	keys int
}

func (s *busyScreen) ID() ScreenID           { return s.id }
func (s *busyScreen) SetSize(int, int)       {}
func (s *busyScreen) Update(tea.Msg) tea.Cmd { return nil }
func (s *busyScreen) View() string           { return "loading" }
func (s *busyScreen) Actions() []Action      { return nil }
func (s *busyScreen) DetailView() string     { return "Busy details" }
func (s *busyScreen) TransientActive() bool  { return true }
func (s *busyScreen) HandleKey(tea.KeyPressMsg) KeyResult {
	s.keys++
	return KeyResult{Consumed: true}
}

func TestFocusedPaneOwnsKeysWhileWorkspaceScreenIsBusy(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 140, Height: 40}} {
		m := updateModel(t, newModel(ThemeLoader{NoColor: true}), size)
		busy := &busyScreen{id: ScreenCapture}
		m.screens[ScreenCapture] = busy
		m.focus = focusSidebar
		m = press(t, m, tea.KeyPressMsg{Code: 'j'})
		if m.screenID() != ScreenCapture {
			t.Fatalf("%dx%d setup: j should land on Capture, got %s", size.Width, size.Height, m.screenID())
		}
		m = press(t, m, tea.KeyPressMsg{Code: 'j'})
		if m.screenID() != ScreenRestore || busy.keys != 0 {
			t.Fatalf("%dx%d: sidebar j was swallowed by the busy workspace screen (screen=%s, busy saw %d keys)", size.Width, size.Height, m.screenID(), busy.keys)
		}
		if size.Width == 80 {
			m = press(t, m, tea.KeyPressMsg{Code: 'k'})
			m = press(t, m, keyEnter)
			if m.focus != focusWorkspace || busy.keys != 0 {
				t.Fatalf("compact Enter should close Navigation even while Capture is busy (focus=%d, busy saw %d keys)", m.focus, busy.keys)
			}
		}
	}
}

func TestDetailsFooterOffersOnlyKeysThatWorkThere(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 140, Height: 40}} {
		m := updateModel(t, newModel(ThemeLoader{NoColor: true}), size)
		m.screens[ScreenConfig] = &configScreen{Config: screens.NewConfig(nil)}
		m.selected = screenIndex(t, ScreenConfig)
		m.focus = focusWorkspace
		if footer := m.footer(); !strings.Contains(footer, "Switch tab") {
			t.Fatalf("%dx%d: workspace footer should list Config actions: %q", size.Width, size.Height, footer)
		}
		m = press(t, m, keyRight)
		if m.focus != focusDetails {
			t.Fatalf("%dx%d: setup could not focus Details", size.Width, size.Height)
		}
		footer := m.footer()
		if strings.Contains(footer, "Switch tab") || !strings.Contains(footer, "Scroll") {
			t.Fatalf("%dx%d: Details footer must offer pane keys, not workspace actions: %q", size.Width, size.Height, footer)
		}
		if !strings.Contains(strings.Join(m.helpLines(), "\n"), "View Config diff") {
			t.Fatalf("%dx%d: help should still list every Config key while Details has focus", size.Width, size.Height)
		}
	}
}
