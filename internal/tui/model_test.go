package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	blueprintmodel "github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestLayout(t *testing.T) {
	for _, test := range []struct {
		width, height int
		want          LayoutMode
	}{{140, 40, LayoutThreePane}, {100, 30, LayoutTwoPane}, {80, 24, LayoutCompact}, {60, 15, LayoutTooSmall}} {
		if got := layoutForSize(test.width, test.height).mode; got != test.want {
			t.Errorf("%dx%d = %s, want %s", test.width, test.height, got, test.want)
		}
	}
}

func TestViewRendersMultiPaneLayouts(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 140, Height: 40}, {Width: 100, Height: 30}} {
		m := updateModel(t, newModel(ThemeLoader{NoColor: true}), size)
		if view := m.View().Content; !strings.Contains(view, "Overview") {
			t.Fatalf("%dx%d view did not contain active screen: %q", size.Width, size.Height, view)
		}
	}
}

func TestPaneRowsAreBoundedToTerminalWidth(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 140, Height: 40}, {Width: 100, Height: 30}} {
		m := updateModel(t, newModel(ThemeLoader{NoColor: true}), size)
		for _, line := range strings.Split(m.View().Content, "\n") {
			if got := lipgloss.Width(line); got > size.Width {
				t.Fatalf("%d-column view produced %d-cell row: %q", size.Width, got, line)
			}
		}
	}
}

func TestWorkspaceShowsActiveScreenDescriptionAndAccountsForItsHeight(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	width, _ := components.InteriorSize(layoutForSize(m.width, m.height).workspaceWidth, layoutForSize(m.width, m.height).contentHeight)
	overviewDescription := screenInfo(ScreenOverview).Short
	assertDescriptionVisibleOnce(t, m.View().Content, overviewDescription, width)
	m.selectScreen(ScreenConfig)
	configDescription := screenInfo(ScreenConfig).Short
	assertDescriptionVisibleOnce(t, m.View().Content, configDescription, width)
	if strings.Contains(m.activeScreen().(DetailView).DetailView(), configDescription) {
		t.Fatal("description appeared in details")
	}
	width, height := m.screenSize(ScreenConfig)
	placeholder := m.screens[ScreenConfig].(*placeholderScreen)
	if placeholder.width != width || placeholder.height != height {
		t.Fatalf("config size = %dx%d, want %dx%d", placeholder.width, placeholder.height, width, height)
	}
}

func assertDescriptionVisibleOnce(t *testing.T, view, description string, width int) {
	t.Helper()
	for _, line := range components.WrapText(description, width) {
		if got := strings.Count(view, line); got != 1 {
			t.Fatalf("description line %q count = %d, want 1", line, got)
		}
	}
}

func TestScreenMessagesKeepTheirOwnerAfterNavigation(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	owner := &recordingScreen{id: ScreenResources}
	m.screens[ScreenResources] = owner
	m.selectScreen(ScreenSync)
	updated, followup := m.Update(screenMsg{Screen: ScreenResources, Msg: ownedTestMsg{step: 1}})
	m = updated.(model)
	if len(owner.steps) != 1 || owner.steps[0] != 1 {
		t.Fatalf("non-active owner did not receive message: %#v", owner.steps)
	}
	if followup == nil {
		t.Fatal("screen follow-up was not rewrapped")
	}
	if _, ok := followup().(screenMsg); !ok {
		t.Fatal("screen follow-up lost its owner envelope")
	}
}

func TestWelcomeFooterPreservesChooserModes(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})

	m.welcomeChooser = true
	m.welcomeStep = "choose"
	if got := m.welcomeFooter(); got != "up/down select   enter continue   esc quit" {
		t.Fatalf("chooser footer = %q", got)
	}

	m.welcomeStep = "open-path"
	if got := m.welcomeFooter(); got != "enter continue   esc back" {
		t.Fatalf("path footer = %q", got)
	}

	m.welcomeChooser = false
	if got := m.welcomeFooter(); got != "enter create   esc quit" {
		t.Fatalf("create footer = %q", got)
	}
}

func TestModalFooterTextPreservesCurrentModes(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})

	m.modal = modalPalette
	if got := m.modalFooter(); got != "type search   up/down select   enter run   esc close" {
		t.Fatalf("palette footer = %q", got)
	}

	m.modal = modalHelp
	m.requestedModal = nil
	if got := m.modalFooter(); got != "type search   up/down scroll   esc close" {
		t.Fatalf("help footer = %q", got)
	}

	m.requestedModal = &ModalRequest{Content: "confirm"}
	if got := m.modalFooter(); got != "enter confirm   esc cancel" {
		t.Fatalf("confirm footer = %q", got)
	}

	m.requestedModal = &ModalRequest{Input: "value"}
	if got := m.modalFooter(); got != "enter save   esc cancel" {
		t.Fatalf("input footer = %q", got)
	}
}

func TestRequestedModalResponseReturnsToRequestingScreenAfterNavigation(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	requester := &allMessageRecordingScreen{id: ScreenResources}
	m.screens[ScreenResources] = requester

	updated, _ := m.Update(screenMsg{
		Screen: ScreenResources,
		Msg: components.ModalRequest{
			Title:   "Confirm",
			Content: "Proceed?",
		},
	})
	m = updated.(model)
	m.selected = screenIndex(t, ScreenSync)

	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(requester.messages) != 1 {
		t.Fatalf("requester messages = %#v", requester.messages)
	}
	key, ok := requester.messages[0].(tea.KeyPressMsg)
	if !ok || key.String() != "enter" {
		t.Fatalf("modal response = %#v, want enter key", requester.messages[0])
	}
}

type allMessageRecordingScreen struct {
	id       ScreenID
	messages []tea.Msg
}

func (s *allMessageRecordingScreen) ID() ScreenID     { return s.id }
func (s *allMessageRecordingScreen) SetSize(int, int) {}
func (s *allMessageRecordingScreen) Update(msg tea.Msg) tea.Cmd {
	s.messages = append(s.messages, msg)
	return nil
}
func (s *allMessageRecordingScreen) View() string      { return "recording" }
func (s *allMessageRecordingScreen) Actions() []Action { return nil }

func TestRootOwnedWrappedNoticeIsHandledOnce(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	owner := &allMessageRecordingScreen{id: ScreenResources}
	m.screens[ScreenResources] = owner

	updated, _ := m.Update(screenMsg{
		Screen: ScreenResources,
		Msg:    screens.Notice{Message: "saved"},
	})
	m = updated.(model)

	if m.notification != "saved" {
		t.Fatalf("notification = %q, want saved", m.notification)
	}
	if len(owner.messages) != 0 {
		t.Fatalf("root-owned notice leaked back to screen: %#v", owner.messages)
	}
}

func TestMachineMutationCompletionUpdatesRootAndReturnsToOrigin(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	owner := &allMessageRecordingScreen{id: ScreenMachines}
	m.screens[ScreenMachines] = owner

	updated, _ := m.Update(screenMsg{
		Screen: ScreenMachines,
		Msg: screens.MachineMutationComplete{
			Notice: "Machine added.",
		},
	})
	m = updated.(model)

	if m.notification != "Machine added." {
		t.Fatalf("notification = %q", m.notification)
	}
	if len(owner.messages) != 1 {
		t.Fatalf("completion did not return to Machines: %#v", owner.messages)
	}
}

func TestBatchedScreenCommandsKeepTheirOwner(t *testing.T) {
	cmd := wrapScreenCmd(ScreenResources, tea.Batch(
		func() tea.Msg { return ownedTestMsg{step: 1} },
		func() tea.Msg { return ownedTestMsg{step: 2} },
	))
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("wrapped command = %#v", batch)
	}
	for _, child := range batch {
		wrapped, ok := child().(screenMsg)
		if !ok || wrapped.Screen != ScreenResources {
			t.Fatalf("batch child lost owner: %#v", wrapped)
		}
	}
}

func TestCompactSidebarIsVisibleWhenToggled(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.focus == focusSidebar {
		t.Fatal("compact layout retained invisible sidebar focus")
	}
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.sidebarOpen || m.focus != focusSidebar || !strings.Contains(m.View().Content, "Overview") {
		t.Fatal("compact sidebar did not open visibly")
	}
}

func TestSidebarSectionNavigation(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	m.focus = focusSidebar
	for _, test := range []struct {
		start ScreenID
		key   rune
		want  ScreenID
	}{
		{ScreenOverview, ']', ScreenCapture},
		{ScreenRestore, ']', ScreenPackages},
		{ScreenThemes, '[', ScreenCapture},
		{ScreenConfig, ']', ScreenMachines},
		{ScreenSync, '[', ScreenConfig},
		{ScreenSync, ']', ScreenSync},
		{ScreenCapture, '[', ScreenOverview},
	} {
		m.selected = screenIndex(t, test.start)
		m = updateModel(t, m, tea.KeyPressMsg{Code: test.key})
		if got := m.screenID(); got != test.want || m.focus != focusSidebar {
			t.Fatalf("%s %q = %s (focus %d), want %s sidebar", test.start, test.key, got, m.focus, test.want)
		}
	}
	m.focus = focusWorkspace
	m.selected = screenIndex(t, ScreenOverview)
	m = updateModel(t, m, tea.KeyPressMsg{Code: ']'})
	if m.screenID() != ScreenOverview {
		t.Fatal("workspace bracket changed ungrouped screen")
	}

	m = updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = updateModel(t, m, tea.KeyPressMsg{Code: ']'})
	if !m.sidebarOpen || m.screenID() != ScreenCapture || m.focus != focusSidebar {
		t.Fatalf("compact sidebar section jump failed: open=%v screen=%s focus=%d", m.sidebarOpen, m.screenID(), m.focus)
	}
}

func TestGroupNavigationBindingsFollowFocusAndStayOutOfFooter(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m.screens[ScreenConfig] = &configScreen{Config: screens.NewConfig(nil)}
	m.selected = screenIndex(t, ScreenConfig)
	m.focus = focusWorkspace
	assertBindingLabels(t, m.bindings(), "Previous group", "Next group")
	if footer := m.footer(); strings.Contains(footer, "Previous group") || strings.Contains(footer, "Next group") {
		t.Fatalf("group bindings appeared in footer: %q", footer)
	}
	m.focus = focusDetails
	assertBindingLabels(t, m.bindings())
	m.focus = focusSidebar
	assertBindingLabels(t, m.bindings(), "Previous sidebar section", "Next sidebar section")
}

func assertBindingLabels(t *testing.T, bindings []Binding, want ...string) {
	t.Helper()
	labels := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		labels = append(labels, binding.Label)
	}
	for _, label := range want {
		if !containsString(labels, label) {
			t.Fatalf("bindings %q missing %q", labels, label)
		}
	}
	if len(want) == 0 {
		for _, label := range labels {
			if label == "Previous group" || label == "Next group" {
				t.Fatalf("details retained workspace binding %q", label)
			}
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func screenIndex(t *testing.T, id ScreenID) int {
	t.Helper()
	for index, candidate := range screenOrder {
		if candidate == id {
			return index
		}
	}
	t.Fatalf("screen %s not found", id)
	return 0
}

func TestScreenKeyOwnershipPrecedesRootFocusAndOverlays(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	m.screens[ScreenResources] = &resourcesScreen{Resources: screens.NewResources(nil)}
	m.selectScreen(ScreenResources)
	m.focus = focusWorkspace
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focus != focusWorkspace {
		t.Fatal("resources tab was taken by root focus")
	}

	m.screens[ScreenSync] = &syncScreen{Sync: &screens.Sync{}}
	m.selectScreen(ScreenSync)
	m.focus = focusWorkspace
	m = updateModel(t, m, tea.KeyPressMsg{Code: 'l'})
	if m.focus != focusWorkspace {
		t.Fatal("sync l was taken by root focus")
	}

	input := &inputScreen{id: ScreenSync, active: true}
	m.screens[ScreenSync] = input
	m = updateModel(t, m, tea.KeyPressMsg{Code: '?'})
	if m.modal != modalNone || input.keys != "?" {
		t.Fatal("screen input did not retain question mark")
	}
}

func TestModalLeavesBaseViewVisibleAndViewportScrolls(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	base := m.View().Content
	m = updateModel(t, m, tea.KeyPressMsg{Code: ':'})
	if !strings.Contains(m.View().Content, "Command palette") || !strings.Contains(m.View().Content, "Overview") {
		t.Fatal("modal did not retain base view")
	}
	if got, want := len(strings.Split(m.View().Content, "\n")), len(strings.Split(base, "\n")); got != want {
		t.Fatalf("modal appended to the base: rows=%d want=%d", got, want)
	}
	var viewport verticalViewport
	viewport.move(100, 30, 4)
	if viewport.offset != 26 {
		t.Fatalf("viewport offset = %d, want 26", viewport.offset)
	}
}

func TestModelIntegrationRouteUsesSharedSessionAcrossRefreshes(t *testing.T) {
	session := integrationSession(t)
	m := newModelWithSession(ThemeLoader{NoColor: true}, session)
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

	// Overview targets route through the root model to the same shared session.
	m = updateModel(t, m, screens.OverviewTarget{Target: "config", Ref: ".config/example/settings.toml"})
	if m.screenID() != ScreenConfig {
		t.Fatalf("config route selected %s", m.screenID())
	}
	configScreen := m.activeScreen().(*configScreen)
	consumeScreenCmd(t, &m, configScreen.Init())
	if view := configScreen.View(); !strings.Contains(view, "Needs review") || !strings.Contains(view, ".config/example/settings.toml") || !strings.Contains(view, "Auto") {
		t.Fatalf("config semantic markers missing: %q", view)
	}
	consumeScreenCmd(t, &m, configScreen.Update(tea.KeyPressMsg{Code: 'i'}))
	if got := session.Profile().Config.Included; len(got) != 1 || got[0] != ".config/example/settings.toml" {
		t.Fatalf("config policy was not persisted: %#v", got)
	}

	m = updateModel(t, m, screens.OverviewTarget{Target: "resources", Ref: "projects"})
	resourcesScreen := m.activeScreen().(*resourcesScreen)
	consumeScreenCmd(t, &m, resourcesScreen.Init())
	if !strings.Contains(resourcesScreen.View(), "projects") {
		t.Fatalf("resource inspection lost shared profile data: %q", resourcesScreen.View())
	}
	m = updateModel(t, m, screens.OverviewTarget{Target: "machines", Ref: "desktop:projects"})
	if view := m.activeScreen().View(); !strings.Contains(view, "notes") || !strings.Contains(view, "~/Notes") || !strings.Contains(view, "~/Code/Notes") || !strings.Contains(view, "override") {
		t.Fatalf("machine mapping lost shared profile data: %q", view)
	}

	m.selectScreen(ScreenRestore)
	restoreScreen := m.activeScreen().(*restoreScreen)
	consumeScreenCmd(t, &m, restoreScreen.Update(tea.KeyPressMsg{Code: 'f'}))
	if !strings.Contains(restoreScreen.View(), "active: [Forced]") {
		t.Fatalf("restore mode did not toggle: %q", restoreScreen.View())
	}
	m.selectScreen(ScreenSync)
	syncScreen := m.activeScreen().(*syncScreen)
	consumeScreenCmd(t, &m, syncScreen.Init())
	if !strings.Contains(syncScreen.View(), "managed") {
		t.Fatalf("sync did not observe managed profile changes: %q", syncScreen.View())
	}

	// A refresh reloads persisted policy while preserving resource and machine data.
	consumeScreenCmd(t, &m, resourcesScreen.Init())
	if !strings.Contains(resourcesScreen.View(), "projects") || session.Profile().Machines.Items[0].ResourcePaths[0].Path != "~/Code/Notes" {
		t.Fatal("refresh did not preserve shared workflow state")
	}
}

type ownedTestMsg struct{ step int }
type recordingScreen struct {
	id      ScreenID
	steps   []int
	inits   int
	actions []Action
}

func (s *recordingScreen) ID() ScreenID     { return s.id }
func (s *recordingScreen) SetSize(int, int) {}
func (s *recordingScreen) Init() tea.Cmd    { s.inits++; return nil }
func (s *recordingScreen) Update(msg tea.Msg) tea.Cmd {
	if owned, ok := msg.(ownedTestMsg); ok {
		s.steps = append(s.steps, owned.step)
		return func() tea.Msg { return ownedTestMsg{step: owned.step + 1} }
	}
	return nil
}
func (s *recordingScreen) View() string      { return "recording" }
func (s *recordingScreen) Actions() []Action { return s.actions }

func TestModelInitializesScreensOnFirstVisitOnly(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	overview := &recordingScreen{id: ScreenOverview}
	config := &recordingScreen{id: ScreenConfig}
	m.screens[ScreenOverview], m.screens[ScreenConfig] = overview, config
	m.Init()
	if overview.inits != 1 || config.inits != 0 {
		t.Fatalf("startup init counts: overview=%d config=%d", overview.inits, config.inits)
	}
	m.selectScreen(ScreenConfig)
	m.selectScreen(ScreenConfig)
	if config.inits != 1 {
		t.Fatalf("config initialized %d times", config.inits)
	}
}

func TestModelReloadRefreshesInitializedScreensAfterPullAndLazyGit(t *testing.T) {
	m := newModelWithSession(ThemeLoader{NoColor: true}, integrationSession(t))
	overview := &recordingScreen{id: ScreenOverview}
	config := &recordingScreen{id: ScreenConfig}
	m.screens[ScreenOverview], m.screens[ScreenConfig] = overview, config
	m.initialized[ScreenOverview] = true

	updated, _ := m.updateScreenMsg(screenMsg{Screen: ScreenSync, Msg: screens.SessionReloadNeeded{Reason: "profile Git pull"}})
	m = updated.(model)
	if overview.inits != 1 || config.inits != 0 || !strings.Contains(m.notification, "profile Git pull") {
		t.Fatalf("pull reload: overview=%d config=%d notice=%q", overview.inits, config.inits, m.notification)
	}
	updated, _ = m.Update(handoffFinishedMsg{Kind: "lazygit"})
	m = updated.(model)
	if overview.inits != 2 || config.inits != 0 || !strings.Contains(m.notification, "LazyGit") {
		t.Fatalf("LazyGit reload: overview=%d config=%d notice=%q", overview.inits, config.inits, m.notification)
	}
}

func TestCaptureCleanupWarningRefreshesCommittedState(t *testing.T) {
	m := newModelWithSession(ThemeLoader{NoColor: true}, integrationSession(t))
	updated, cmd := m.handleCaptureComplete(screens.CaptureComplete{Providers: []string{"packages"}, Warning: errors.New("backup cleanup")})
	m = updated.(model)
	if cmd == nil || !strings.Contains(m.notification, "Capture applied; cleanup warning: backup cleanup") || strings.Contains(m.notification, "Capture failed") {
		t.Fatalf("cmd=%v notification=%q", cmd != nil, m.notification)
	}
}

type inputScreen struct {
	id     ScreenID
	active bool
	keys   string
}

func (s *inputScreen) ID() ScreenID     { return s.id }
func (s *inputScreen) SetSize(int, int) {}
func (s *inputScreen) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		s.keys += key.String()
	}
	return nil
}
func (s *inputScreen) View() string          { return "input" }
func (s *inputScreen) Actions() []Action     { return nil }
func (s *inputScreen) TransientActive() bool { return s.active }
func (s *inputScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}

type countingScreen struct {
	id   ScreenID
	down int
}

func (s *countingScreen) ID() ScreenID     { return s.id }
func (s *countingScreen) SetSize(int, int) {}
func (s *countingScreen) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "down" {
		s.down++
	}
	return nil
}
func (s *countingScreen) View() string      { return "counting" }
func (s *countingScreen) Actions() []Action { return nil }
func (s *countingScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}

type resultScreen struct {
	id      ScreenID
	consume map[string]bool
	keys    string
}

func (s *resultScreen) ID() ScreenID     { return s.id }
func (s *resultScreen) SetSize(int, int) {}
func (s *resultScreen) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		s.keys += key.String()
	}
	return nil
}
func (s *resultScreen) View() string      { return "result" }
func (s *resultScreen) Actions() []Action { return nil }
func (s *resultScreen) HandleKey(key tea.KeyPressMsg) KeyResult {
	if !s.consume[key.String()] {
		return KeyResult{}
	}
	return KeyResult{Consumed: true, Cmd: s.Update(key)}
}

func TestNoColorViewsKeepSemanticMarkers(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if strings.Contains(m.View().Content, "\x1b[") || !strings.Contains(m.View().Content, "✓ overview clean") {
		t.Fatalf("overview is not understandable without colour: %q", m.View().Content)
	}
}

func TestRootNotificationSanitizesControls(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	m.notification = "failed\nnext\x1b[31m"
	view := m.View().Content
	if strings.Contains(view, "\x1b") || !strings.Contains(view, "failed?next?[31m") {
		t.Fatalf("unsafe notification=%q", view)
	}
}

func TestRootHeaderSanitizesProfileName(t *testing.T) {
	session := integrationSession(t)
	data := session.Profile()
	data.Manifest.Profile.Name = "profile\nname\x1b"
	if err := profile.Save(session.ProfileDir(), data); err != nil {
		t.Fatal(err)
	}
	if err := session.Reload(); err != nil {
		t.Fatal(err)
	}
	m := updateModel(t, newModelWithSession(ThemeLoader{NoColor: true}, session), tea.WindowSizeMsg{Width: 100, Height: 30})
	header := m.header()
	if strings.Contains(header, "\x1b") || !strings.Contains(header, "profile?name?") {
		t.Fatalf("unsafe header=%q", header)
	}
}

func TestResizeSafety(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 0, Height: 0}, {Width: 1, Height: 1}, {Width: 69, Height: 17}, {Width: 70, Height: 18}, {Width: 80, Height: 24}, {Width: 100, Height: 30}, {Width: 140, Height: 40}, {Width: 1000, Height: 1000}} {
		m := updateModel(t, newModel(ThemeLoader{NoColor: true}), size)
		for _, key := range []rune{'j', 'k', 'l', 'h', ':'} {
			m = updateModel(t, m, tea.KeyPressMsg{Code: key})
		}
		_ = m.View()
	}
}

func FuzzLayoutDimensions(f *testing.F) {
	for _, size := range [][2]int{{140, 40}, {100, 30}, {80, 24}, {60, 15}} {
		f.Add(size[0], size[1])
	}
	f.Fuzz(func(t *testing.T, width, height int) {
		layout := layoutForSize(width, height)
		if layout.sidebarWidth < 0 || layout.workspaceWidth < 0 || layout.detailsWidth < 0 || layout.contentHeight < 0 {
			t.Fatalf("negative layout dimensions: %#v", layout)
		}
	})
}

func TestNavigation(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updateModel(t, m, tea.KeyPressMsg{Code: 'j'})
	if m.screenID() != ScreenCapture {
		t.Fatalf("screen = %s", m.screenID())
	}
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focus != focusWorkspace {
		t.Fatalf("focus = %d", m.focus)
	}
	m = updateModel(t, m, tea.KeyPressMsg{Code: ':'})
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.paletteOpen {
		t.Fatal("escape did not close the palette")
	}
	_, command := m.Update(tea.KeyPressMsg{Code: 'q'})
	if command == nil || command() != tea.Quit() {
		t.Fatal("q did not quit without a transient")
	}
}

func TestKeyIsDeliveredToClaimingScreenOnce(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, id := range screenOrder {
		screen := &countingScreen{id: id}
		m.screens[id] = screen
		m.selectScreen(id)
		m.focus = focusWorkspace
		m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
		if screen.down != 1 {
			t.Fatalf("%s down deliveries = %d, want 1", id, screen.down)
		}
	}
}

func TestQuitDefersToTransientInput(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	input := &inputScreen{id: ScreenOverview, active: true}
	m.screens[ScreenOverview] = input
	_, command := m.Update(tea.KeyPressMsg{Code: 'q'})
	if command != nil || input.keys != "q" {
		t.Fatalf("transient q was not screen-owned: command=%v keys=%q", command != nil, input.keys)
	}
}

func TestRootKeyRoutingPrecedenceAndFallback(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	screen := &resultScreen{id: ScreenOverview, consume: map[string]bool{"x": true}}
	m.screens[ScreenOverview] = screen
	m.focus = focusWorkspace

	m = updateModel(t, m, tea.KeyPressMsg{Code: 'x'})
	if screen.keys != "x" {
		t.Fatalf("consumed key deliveries=%q, want one", screen.keys)
	}
	before := m.selected
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if screen.keys != "x" || m.selected != before {
		t.Fatalf("declined workspace key was delivered or moved root state: keys=%q selected=%d", screen.keys, m.selected)
	}

	m.openModal(modalPalette)
	m = updateModel(t, m, tea.KeyPressMsg{Code: 'x'})
	if screen.keys != "x" {
		t.Fatalf("modal key leaked to screen: %q", screen.keys)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil || cmd() != tea.Quit() || updated.(model).modal != modalPalette || screen.keys != "x" {
		t.Fatalf("ctrl-c did not preempt modal/screen: modal=%v keys=%q", updated.(model).modal, screen.keys)
	}
}

func TestPanelsAndSidebarSelectionRender(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 140, Height: 40})
	view := m.View().Content
	for _, want := range []string{"┌ Navigation", "┌ Overview", "> Overview"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing panel/sidebar marker %q", want)
		}
	}
	if !strings.Contains(view, "Overview details") {
		t.Fatal("overview did not render its details pane")
	}
}

func TestFooterTracksResourceStateAndModalInput(t *testing.T) {
	m := updateModel(t, newModelWithSession(ThemeLoader{NoColor: true}, integrationSession(t)), tea.WindowSizeMsg{Width: 100, Height: 30})
	m.selectScreen(ScreenResources)
	resources := m.activeScreen().(*resourcesScreen)
	consumeScreenCmd(t, &m, resources.Init())
	// A tracked item exposes contextual actions; Discover is entered directly by Tab.
	if footer := m.footer(); !strings.Contains(footer, "s Change strategy") || !strings.Contains(footer, "u Untrack resource") {
		t.Fatalf("tracked footer=%q", footer)
	}
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if footer := m.footer(); strings.Contains(footer, "Change strategy") || strings.Contains(footer, "Browse resource") {
		t.Fatalf("discover footer=%q", footer)
	}
	m.requestedModal = &ModalRequest{Title: "Confirm", Content: "confirm"}
	m.openModal(modalHelp)
	if footer := m.footer(); footer != "enter confirm   esc cancel" {
		t.Fatalf("modal footer=%q", footer)
	}
}

func TestScreenModalRequestUsesConfirmationOverlay(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	updated, _ := m.updateScreenMsg(screenMsg{Screen: ScreenOverview, Msg: components.ModalRequest{Title: "Confirm capture", Content: "Capture changes?"}})
	m = updated.(model)
	if m.modal != modalConfirm || !strings.Contains(m.View().Content, "Confirm capture") {
		t.Fatalf("modal=%d view=%q", m.modal, m.View().Content)
	}
}

func TestDirectScreenKeyOpensRootConfirmationModal(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	m.screens[ScreenOverview] = &modalKeyScreen{}
	m.selectScreen(ScreenOverview)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c'})
	m = updated.(model)
	consumeScreenCmd(t, &m, cmd)
	if m.modal != modalConfirm || !strings.Contains(m.View().Content, "Confirm direct action") || m.footer() != "enter confirm   esc cancel" {
		t.Fatalf("modal=%d footer=%q view=%q", m.modal, m.footer(), m.View().Content)
	}
}

type modalKeyScreen struct{}

func (*modalKeyScreen) ID() ScreenID           { return ScreenOverview }
func (*modalKeyScreen) SetSize(int, int)       {}
func (*modalKeyScreen) Update(tea.Msg) tea.Cmd { return nil }
func (*modalKeyScreen) View() string           { return "direct action" }
func (*modalKeyScreen) Actions() []Action      { return nil }
func (*modalKeyScreen) HandleKey(tea.KeyPressMsg) KeyResult {
	return KeyResult{Consumed: true, Cmd: func() tea.Msg { return components.ModalRequest{Title: "Confirm direct action", Content: "Proceed?"} }}
}

func TestCommandPalette(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m = updateModel(t, m, tea.KeyPressMsg{Code: ':'})
	for _, key := range "sync" {
		m = updateModel(t, m, textKey(key))
	}
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screenID() != ScreenSync || m.paletteOpen {
		t.Fatalf("screen=%s palette=%t", m.screenID(), m.paletteOpen)
	}
}

func TestEmptySearchInputsHaveNoPlaceholderCharacter(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, tea.KeyPressMsg{Code: ':'})
	if view := m.View().Content; strings.Contains(view, "Search: S") {
		t.Fatalf("palette placeholder leaked into empty input: %q", view)
	}
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updateModel(t, m, tea.KeyPressMsg{Code: '?'})
	if view := m.View().Content; strings.Contains(view, "Search: S") {
		t.Fatalf("help placeholder leaked into empty input: %q", view)
	}
}

func TestConfigFilterOwnsReservedPrintableKeysAtRoot(t *testing.T) {
	m := updateModel(t, newModelWithSession(ThemeLoader{NoColor: true}, integrationSession(t)), tea.WindowSizeMsg{Width: 100, Height: 30})
	consumeScreenCmd(t, &m, m.selectScreen(ScreenConfig))
	m = updateModel(t, m, tea.KeyPressMsg{Code: '/'})
	for _, key := range "q?:hl[]" {
		m = updateModel(t, m, textKey(key))
	}
	if !strings.Contains(m.activeScreen().View(), "Filter: q?:hl[]") || m.modal != modalNone || m.focus != focusWorkspace {
		t.Fatalf("view=%q modal=%d focus=%d", m.activeScreen().View(), m.modal, m.focus)
	}
}

func TestHelp(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.screens[ScreenConfig] = &configScreen{Config: screens.NewConfig(nil)}
	m.selectScreen(ScreenConfig)
	m.focus = focusDetails
	m = updateModel(t, m, tea.KeyPressMsg{Code: '?'})
	view := m.View().Content
	long := screenInfo(ScreenConfig).Long
	if !m.helpOpen || !strings.Contains(view, "CONFIG") || !strings.Contains(view, "View Config diff") {
		t.Fatalf("help is not contextual to screen and focus: %q", view)
	}
	lines := m.helpLines()
	for _, want := range []string{"CONFIG", "Search:", "KEYS", "GLOBAL", "Focus: details"} {
		if !strings.Contains(strings.Join(lines, "\n"), want) {
			t.Fatalf("default help missing %q: %q", want, lines)
		}
	}
	for _, line := range components.WrapText(long, layoutForSize(m.width, m.height).workspaceWidth-4) {
		if !strings.Contains(strings.Join(lines, "\n"), line) {
			t.Fatalf("default help missing description line %q: %q", line, lines)
		}
	}
	if !(indexOf(lines, "CONFIG") < indexOf(lines, "Search:") && indexOf(lines, "Search:") < indexOf(lines, "KEYS") && indexOf(lines, "KEYS") < indexOf(lines, "GLOBAL") && indexOf(lines, "GLOBAL") < indexOf(lines, "Focus: details")) {
		t.Fatalf("help layout order=%q", lines)
	}
	for _, key := range "diff" {
		m = updateModel(t, m, textKey(key))
	}
	view = m.View().Content
	if !strings.Contains(view, "Config is about your dotfiles") || !strings.Contains(view, "View Config diff") || strings.Contains(view, "Cycle Config policy") {
		t.Fatalf("help search did not filter actions: %q", view)
	}
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.helpOpen {
		t.Fatal("help did not close")
	}
}

func TestPaletteScreenAliases(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	for query, want := range map[string]ScreenID{
		"dotfiles": ScreenConfig,
		"rebuild":  ScreenRestore,
		"commit":   ScreenSync,
		"laptop":   ScreenMachines,
		"scripts":  ScreenHooks,
	} {
		matches := filterActions(m.actions(), query)
		found := false
		for _, match := range matches {
			found = found || match.Screen == want
		}
		if !found {
			t.Errorf("%q matches=%#v, want %s", query, matches, want)
		}
	}
}

func indexOf(lines []string, prefix string) int {
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return i
		}
	}
	return len(lines)
}

func TestPaletteDoesNotRunDisabledAction(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m.screens[ScreenConfig] = &configScreen{Config: screens.NewConfig(nil)}
	m.selectScreen(ScreenConfig)
	m = updateModel(t, m, tea.KeyPressMsg{Code: ':'})
	for _, key := range "config.edit" {
		m = updateModel(t, m, textKey(key))
	}
	if items := filterActions(m.actions(), m.paletteQuery); len(items) != 1 || items[0].Enabled || items[0].DisabledReason == "" {
		t.Fatalf("disabled palette action is not accurately described: %#v", items)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil || m.modal != modalPalette {
		t.Fatal("disabled palette action ran or closed the palette")
	}
}

func TestPaletteSearchStartsImmediatelyAndClosesWithEscape(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, tea.KeyPressMsg{Code: ':'})
	if !m.paletteFiltering || !strings.Contains(m.View().Content, "Search:") {
		t.Fatal("palette did not open in search mode")
	}
	for _, key := range "jk" {
		m = updateModel(t, m, textKey(key))
	}
	if m.paletteQuery != "jk" {
		t.Fatalf("search query=%q", m.paletteQuery)
	}
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.paletteOpen {
		t.Fatal("escape did not close palette")
	}
}

func TestPaletteInputHandlesUnicodeBackspace(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.KeyPressMsg{Code: ':'})
	m = updateModel(t, m, textKey('é'))
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.paletteQuery != "" {
		t.Fatalf("query = %q", m.paletteQuery)
	}
}

func TestPaletteQueryResetsSelectionAndShowsNoResults(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 18})
	m = updateModel(t, m, tea.KeyPressMsg{Code: ':'})
	m.paletteSelected, m.paletteScroll.offset = 5, 5
	m = updateModel(t, m, textKey('z'))
	if m.paletteSelected != 0 || m.paletteScroll.offset != 0 {
		t.Fatalf("query did not reset palette: selected=%d offset=%d", m.paletteSelected, m.paletteScroll.offset)
	}
	for _, key := range "zzzz-no-command" {
		m = updateModel(t, m, textKey(key))
	}
	if !strings.Contains(m.View().Content, "No matching commands") {
		t.Fatalf("missing no-result message: %q", m.View().Content)
	}
}

func TestPaletteUpKeepsSelectionVisible(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 100, Height: 18})
	actions := make([]Action, 20)
	for i := range actions {
		actions[i] = Action{ID: fmt.Sprintf("item.%d", i), Label: fmt.Sprintf("Item %d", i), Visible: true, Enabled: true, PaletteInitial: true}
	}
	m.screens[ScreenOverview] = &recordingScreen{id: ScreenOverview, actions: actions}
	m = updateModel(t, m, tea.KeyPressMsg{Code: ':'})
	m.paletteSelected, m.paletteScroll.offset = 15, 15
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	height := layoutForSize(m.width, m.height).contentHeight - 2
	if m.paletteSelected != 14 || m.paletteScroll.offset >= 15 || m.paletteSelected < m.paletteScroll.offset || m.paletteSelected >= m.paletteScroll.offset+height {
		t.Fatalf("up did not ensure viewport: selected=%d offset=%d", m.paletteSelected, m.paletteScroll.offset)
	}
}

func TestHelpSearchIncludesBindingKeysLabelsAndBindingOnlyEntries(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m.screens[ScreenPackages] = &providerScreen{Provider: screens.NewProvider(nil, "packages"), id: ScreenPackages}
	m.selectScreen(ScreenPackages)
	for query, want := range map[string]string{"space": "selected package", "enter": "Collapse group", "collapse": "Collapse group", "commands": "Commands"} {
		m.helpQuery = query
		if lines := strings.Join(m.helpLines(), "\n"); !strings.Contains(lines, want) {
			t.Errorf("query %q missing %q: %s", query, want, lines)
		}
	}
}

func TestCaptureActionLabelsUseCategories(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m.screens[ScreenCapture] = &captureScreen{Capture: screens.NewCaptureContext(context.Background(), nil)}
	m.selectScreen(ScreenCapture)
	want := map[string]string{
		"capture.select":      "Toggle category selection",
		"capture.select-all":  "Select changed categories",
		"capture.run":         "Capture selected categories",
		"capture.capture-all": "Capture all categories",
		"capture.refresh":     "Refresh capture status",
	}
	for _, action := range m.actions() {
		if label, ok := want[action.ID]; ok && action.Label != label {
			t.Fatalf("%s label=%q, want %q", action.ID, action.Label, label)
		}
	}
}

func textKey(key rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: key, Text: string(key)} }

func updateModel(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(model)
}

func consumeScreenCmd(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, batchCmd := range batch {
			consumeScreenCmd(t, m, batchCmd)
		}
		return
	}
	updated, next := m.Update(msg)
	*m = updated.(model)
	if next != nil {
		consumeScreenCmd(t, m, next)
	}
}

type configIntegrationProvider struct{}

func (configIntegrationProvider) ID() string                 { return "config" }
func (configIntegrationProvider) Captured(profile.Data) bool { return true }
func (configIntegrationProvider) Capture(context.Context, *profile.Data) (any, []blueprintmodel.Change, error) {
	return nil, nil, nil
}
func (configIntegrationProvider) Diff(context.Context, profile.Data) ([]blueprintmodel.Change, error) {
	return nil, nil
}
func (configIntegrationProvider) DiffWithScan(context.Context, profile.Data) ([]blueprintmodel.Change, config.ScanSummary, error) {
	return nil, config.ScanSummary{Candidates: []config.Candidate{{Path: ".config/example/settings.toml", Classification: config.ConfigAmbiguousBaseline, Reason: "ambiguous baseline"}}}, nil
}
func (configIntegrationProvider) InspectConfig(context.Context, profile.Data, string) (workflow.ConfigInspection, error) {
	return workflow.ConfigInspection{Candidate: config.Candidate{Path: ".config/example/settings.toml", Classification: config.ConfigAmbiguousBaseline, Reason: "ambiguous baseline"}}, nil
}
func (configIntegrationProvider) Plan(context.Context, profile.Data, omarchy.Info, workflow.RestoreMode) (blueprintmodel.RestorePlan, error) {
	return blueprintmodel.RestorePlan{}, nil
}
func (configIntegrationProvider) Verify(context.Context, profile.Data) (blueprintmodel.VerificationResult, error) {
	return blueprintmodel.VerificationResult{OK: true}, nil
}

type resourcesIntegrationProvider struct{}

func (resourcesIntegrationProvider) ID() string                 { return "resources" }
func (resourcesIntegrationProvider) Captured(profile.Data) bool { return true }
func (resourcesIntegrationProvider) Capture(context.Context, *profile.Data) (any, []blueprintmodel.Change, error) {
	return nil, nil, nil
}
func (resourcesIntegrationProvider) Diff(context.Context, profile.Data) ([]blueprintmodel.Change, error) {
	return nil, nil
}
func (resourcesIntegrationProvider) DiffWithGitWorkingState(context.Context, profile.Data) ([]blueprintmodel.Change, map[string]resourcesprovider.GitWorkingSummary, error) {
	return nil, map[string]resourcesprovider.GitWorkingSummary{"projects": {UnstagedTracked: 1}}, nil
}

func integrationSession(t *testing.T) *workflow.Session {
	t.Helper()
	root, state := t.TempDir(), t.TempDir()
	data := profile.New("integration", time.Now())
	data.Manifest.Capture.Config, data.Manifest.Capture.Resources = true, true
	data.Resources.Items = []profile.Resource{
		{ID: "projects", Path: "~/Projects", Strategy: "git"},
		{ID: "notes", Path: "~/Notes", Strategy: "copy"},
	}
	data.Machines.Items = []profile.Machine{{Name: "desktop", ResourcePaths: []profile.MachineResourcePath{{Resource: "notes", Path: "~/Code/Notes"}}}}
	if err := profile.Save(root, data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("managed change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (command.SystemRunner{}).Run(context.Background(), "git", "-C", root, "init"); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(workflow.Dependencies{Runner: command.SystemRunner{}, StateHome: func() (string, error) { return state, nil }}, workflow.Options{ProfileDir: root, ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	session.SetProviders([]workflow.Provider{configIntegrationProvider{}, resourcesIntegrationProvider{}})
	return session
}
