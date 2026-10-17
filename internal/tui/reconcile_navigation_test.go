package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"strings"
	"testing"
)

func TestSemanticSyncAndProfileGitHaveDistinctDestinations(t *testing.T) {
	labels := map[string]ScreenID{}
	for _, info := range screenCatalog {
		if _, exists := labels[info.Label]; exists {
			t.Fatalf("duplicate destination %q", info.Label)
		}
		labels[info.Label] = info.ID
	}
	git, hasGit := labels["Profile Git"]
	semantic, hasSemantic := labels["Sync"]
	if !hasGit || !hasSemantic || git == semantic {
		t.Fatal("missing separate Profile Git and semantic Sync destinations")
	}
	if _, ok := newScreens(context.Background(), integrationSession(t))[semantic].(*placeholderScreen); ok {
		t.Fatal("semantic Sync remains a placeholder with a session")
	}
}

func TestSyncCompletionRefreshesOtherViewsWithoutFetching(t *testing.T) {
	f := &syncNavigationFixture{}
	m := newModel(ThemeLoader{NoColor: true})
	m.screens[ScreenReconcile] = &reconcileScreen{Reconcile: screens.NewReconcileContext(context.Background(), f)}
	m.initialized[ScreenReconcile] = true
	other := &recordingScreen{id: ScreenCapture}
	unopened := &recordingScreen{id: ScreenRestore}
	m.screens[ScreenCapture], m.screens[ScreenRestore] = other, unopened
	m.initialized[ScreenCapture] = true
	_, cmd := m.updateScreenMsg(screenMsg{Screen: ScreenReconcile, Msg: screens.SyncExecutionComplete{Result: workflow.SyncExecutionResult{CursorAdvanced: true}}})
	if cmd == nil {
		t.Fatal("completion did not refresh screens")
	}
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		if batch, ok := cmd().(tea.BatchMsg); ok {
			for _, child := range batch {
				run(child)
			}
		}
	}
	run(cmd)
	if f.calls != 1 || f.fetch || other.inits != 1 || unopened.inits != 0 {
		t.Fatalf("refresh ownership violated: Sync calls=%d fetch=%v initialized=%d unopened=%d", f.calls, f.fetch, other.inits, unopened.inits)
	}
}

type syncNavigationFixture struct {
	fetch bool
	calls int
}

func (f *syncNavigationFixture) InspectSync(_ context.Context, opt workflow.SyncInspectOptions) (syncengine.Plan, error) {
	f.calls++
	f.fetch = opt.Fetch
	return syncengine.Plan{}, nil
}
func (*syncNavigationFixture) ExecuteSync(context.Context, workflow.SyncExecutionRequest) (workflow.SyncExecutionResult, error) {
	return workflow.SyncExecutionResult{}, nil
}
func TestUnrelatedWorkspaceRefreshDoesNotFetchSync(t *testing.T) {
	f := &syncNavigationFixture{}
	m := newModel(ThemeLoader{NoColor: true})
	m.screens[ScreenReconcile] = &reconcileScreen{Reconcile: screens.NewReconcileContext(context.Background(), f)}
	cmd := m.refreshScreen(ScreenReconcile)
	if cmd == nil {
		t.Fatal("missing cached refresh")
	}
	cmd()
	if f.calls != 1 || f.fetch {
		t.Fatal("unrelated workspace refresh fetched Sync upstream")
	}
}

func TestSyncHelpAnswersPlainQuestionsBeforeKeys(t *testing.T) {
	info := screenInfo(ScreenReconcile)
	if info.Short != "Keep your saved Blueprint setup and this computer in sync. Review changes before anything is saved or applied." {
		t.Fatalf("Sync short description = %q", info.Short)
	}
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 120, Height: 40})
	m.screens[ScreenReconcile] = &reconcileScreen{Reconcile: screens.NewReconcileContext(context.Background(), &syncNavigationFixture{})}
	m.selectScreen(ScreenReconcile)
	lines := m.helpLines()
	text := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	for _, want := range []string{
		"Sync helps you use the same Blueprint setup across your computers without accidentally overwriting changes.",
		"Opening Sync does not install, remove, or change anything on your computer.",
		"What does Sync do?", "How is this different from Capture and Restore?", "What is Profile Git?",
		"Does Sync run automatically?", "What happens when I run Sync?", "Can Sync remove or overwrite things?",
		"What happens when I press Check for updates?",
		"Capture saves selected settings from this computer into your Blueprint profile.",
		"Restore applies selected settings from your Blueprint profile to this computer.",
		"Use Profile Git when you want to inspect or manage the profile's Git history directly.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Sync help lacks %q", want)
		}
	}
	if indexOf(lines, "What does Sync do?") > indexOf(lines, "KEYBOARD SHORTCUTS") {
		t.Fatal("keys come before the explanations")
	}
	for _, jargon := range []string{"cursor", "lineage", "bootstrap", "semantic", "upstream", "control plane", "settlement"} {
		if strings.Contains(strings.ToLower(info.Long+info.Short), jargon) {
			t.Fatalf("Sync help uses %q", jargon)
		}
	}
	m.helpQuery = "overwrite"
	if searched := strings.Join(m.helpLines(), "\n"); !strings.Contains(searched, "Can Sync remove or overwrite things?") || strings.Contains(searched, "What is Profile Git?") {
		t.Fatalf("help search did not narrow the explanations: %s", searched)
	}
}

func TestSyncKeysAgreeAcrossFooterHelpAndPalette(t *testing.T) {
	m := updateModel(t, newModel(ThemeLoader{NoColor: true}), tea.WindowSizeMsg{Width: 200, Height: 40})
	screen := &reconcileScreen{Reconcile: screens.NewReconcileContext(context.Background(), &syncNavigationFixture{})}
	m.screens[ScreenReconcile] = screen
	m.selectScreen(ScreenReconcile)
	screen.Update(screen.Init()())
	bindings := map[string]string{}
	for _, binding := range screen.Bindings() {
		bindings[binding.ActionID] = binding.Key
	}
	actions := map[string]Action{}
	for _, action := range screen.Actions() {
		actions[action.ID] = action
		if bindings[action.ID] == "" {
			t.Fatalf("action %s has no key", action.ID)
		}
	}
	if bindings["reconcile.execute"] != "s" || actions["reconcile.execute"].Label != "Review Sync changes" || bindings["reconcile.toggle-group"] != "enter" || bindings["reconcile.refresh"] != "r" || actions["reconcile.refresh"].Label != "Check for updates" {
		t.Fatalf("Sync keys changed meaning: %v", bindings)
	}
	footer := m.footer()
	help := strings.Join(m.helpLines(), "\n")
	palette := map[string]bool{}
	for _, item := range paletteItems(m.actions(), m.bindings()) {
		palette[item.ID] = item.Enabled
	}
	for id, action := range actions {
		inFooter := strings.Contains(footer, bindings[id]+" "+action.Label)
		if action.Enabled != inFooter {
			t.Fatalf("%s enabled=%t but footer=%t: %s", id, action.Enabled, inFooter, footer)
		}
		if !action.Enabled && !strings.Contains(help, action.Label+" (not available now: "+action.DisabledReason+")") {
			t.Fatalf("help does not explain why %s is unavailable", id)
		}
		if enabled, ok := palette[id]; !ok || enabled != action.Enabled {
			t.Fatalf("palette disagrees on %s", id)
		}
	}
}
