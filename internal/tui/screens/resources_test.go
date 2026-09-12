package screens

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestResourceScreenBrowseStrategyConfirmStartsOneTrack(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "candidate"), 0o755); err != nil {
		t.Fatal(err)
	}
	browser := components.NewBrowser(components.BrowseResource, components.BrowserConfig{Home: home, InspectPathCmd: func(id uint64, path string) tea.Cmd {
		return func() tea.Msg {
			return components.BrowserInspectionMsg{RequestID: id, Inspection: workflow.PathInspection{Path: path}}
		}
	}})
	cmd := browser.Init()
	for cmd != nil {
		cmd = browser.Update(cmd())
	}
	screen := &Resources{browser: &browser, phase: resourceBrowse}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if screen.phase != resourceCandidateInspect || screen.candidate.Path == "" || screen.browser.Inspection().Path != screen.candidate.Path {
		t.Fatalf("candidate was not immediately inspected: phase=%q candidate=%#v inspection=%#v", screen.phase, screen.candidate, screen.browser.Inspection())
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen.Update(tea.KeyPressMsg{Code: '1'})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if screen.phase != resourceConfirm || screen.confirm != "track" {
		t.Fatalf("browse flow did not reach track confirmation: phase=%q confirm=%q", screen.phase, screen.confirm)
	}
	first := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	second := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if first == nil || second != nil || screen.phase != resourcePending {
		t.Fatalf("track was not serialized: first=%v second=%v phase=%q", first != nil, second != nil, screen.phase)
	}
}

func TestResourceActionsAreContextual(t *testing.T) {
	screen := &Resources{phase: resourceBrowse, items: []profile.Resource{{ID: "projects", Path: "~/Projects", Strategy: "copy"}}}
	if got := resourceActionIDs(screen.Actions()); !reflect.DeepEqual(got, []string{"discover", "strategy", "untrack", "edit", "open", "copy"}) {
		t.Fatalf("tracked actions=%v", got)
	}
	screen.discover = true
	if got := resourceActionIDs(screen.Actions()); !reflect.DeepEqual(got, []string{"discover"}) {
		t.Fatalf("discover actions=%v", got)
	}
}

func TestResourceScreenTrackedStatusLabels(t *testing.T) {
	screen := &Resources{items: []profile.Resource{
		{ID: "copy", Path: "~/Copy", Strategy: "copy"},
		{ID: "git", Path: "~/Git", Strategy: "git", Dirty: true},
		{ID: "git-diff", Path: "~/Diff", Strategy: "git+diff"},
	}, git: map[string]resourcesprovider.GitWorkingSummary{"git-diff": {StagedTracked: 1, UnstagedTracked: 2, Untracked: []string{"new.txt", "other.txt"}, SelectedUntracked: []string{"new.txt"}}}}
	view := screen.View()
	for _, want := range []string{"copy  ~/Copy  copy", "git  ~/Git  git (dirty: local changes are not captured)", "git-diff  ~/Diff  git+diff (staged:1 unstaged:2 untracked:2 selected:1)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestResourceScreenDownKeepsLongTrackedListSelectionVisible(t *testing.T) {
	items := make([]profile.Resource, 8)
	for i := range items {
		items[i] = profile.Resource{ID: fmt.Sprintf("resource-%d", i), Path: "~/Resource", Strategy: "copy"}
	}
	screen := &Resources{width: 80, height: 6, items: items}
	for range items[1:] {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if screen.selected != len(items)-1 || !strings.Contains(screen.View(), "> resource-7") {
		t.Fatalf("selection=%d view=%q", screen.selected, screen.View())
	}
}

func TestResourceScreenUntrackedSelectorUsesSpaceAndAll(t *testing.T) {
	screen := &Resources{phase: resourceUntracked, untracked: []string{"first.txt", "second.txt"}, chosen: map[string]bool{}}
	screen.Update(tea.KeyPressMsg{Code: ' '})
	if !screen.chosen["first.txt"] {
		t.Fatal("space did not toggle the selected untracked file")
	}
	screen.Update(tea.KeyPressMsg{Code: 'j'})
	screen.Update(tea.KeyPressMsg{Code: 'a'})
	if !screen.chosen["first.txt"] || !screen.chosen["second.txt"] {
		t.Fatalf("a did not select all untracked files: %#v", screen.chosen)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if screen.phase != resourceConfirm || screen.confirm != "track" {
		t.Fatalf("selector did not advance to confirmation: phase=%q confirm=%q", screen.phase, screen.confirm)
	}
}

func TestResourceScreenUntrackWarningPreservesLivePath(t *testing.T) {
	screen := &Resources{items: []profile.Resource{{ID: "projects", Path: "~/Projects"}}, confirm: "untrack"}
	if view := screen.View(); !strings.Contains(view, "live path remains untouched") {
		t.Fatalf("view=%q", view)
	}
}

func resourceActionIDs(actions []ResourceAction) []string {
	ids := make([]string, len(actions))
	for i, action := range actions {
		ids[i] = action.ID
	}
	return ids
}
