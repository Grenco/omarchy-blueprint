package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	blueprintmodel "github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
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
	if view := configScreen.View(); !strings.Contains(view, "> .config/example/settings.toml  ambiguous-baseline") || !strings.Contains(view, "Reason: ambiguous baseline") {
		t.Fatalf("config semantic markers missing: %q", view)
	}
	consumeScreenCmd(t, &m, configScreen.Update(tea.KeyPressMsg{Code: 'i'}))
	consumeScreenCmd(t, &m, configScreen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
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
	if view := m.activeScreen().View(); !strings.Contains(view, "notes -> ~/Code/Notes (override)") {
		t.Fatalf("machine mapping lost shared profile data: %q", view)
	}

	m.selectScreen(ScreenRestore)
	restoreScreen := m.activeScreen().(*restoreScreen)
	consumeScreenCmd(t, &m, restoreScreen.Update(tea.KeyPressMsg{Code: 'f'}))
	if !strings.Contains(restoreScreen.View(), "Restore [Forced]") {
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

func TestNoColorViewsKeepSemanticMarkers(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if strings.Contains(m.View().Content, "\x1b[") || !strings.Contains(m.View().Content, "! attention") {
		t.Fatalf("overview is not understandable without colour: %q", m.View().Content)
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
	if m.screenID() != ScreenPackages {
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
		t.Fatal("q did not quit when no transient was open")
	}
}

func TestCommandPalette(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m = updateModel(t, m, tea.KeyPressMsg{Code: ':'})
	for _, key := range "sync" {
		m = updateModel(t, m, tea.KeyPressMsg{Code: key})
	}
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screenID() != ScreenSync || m.paletteOpen {
		t.Fatalf("screen=%s palette=%t", m.screenID(), m.paletteOpen)
	}
}

func TestHelp(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	m = updateModel(t, m, tea.KeyPressMsg{Code: '?'})
	if !m.helpOpen {
		t.Fatal("help did not open")
	}
	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.helpOpen {
		t.Fatal("help did not close")
	}
}

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
	updated, next := m.Update(cmd())
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
