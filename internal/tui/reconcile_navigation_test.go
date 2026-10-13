package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
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
