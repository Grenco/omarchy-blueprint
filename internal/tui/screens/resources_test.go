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
	deliverResourceBrowser(t, &browser, cmd)
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

func deliverResourceBrowser(t *testing.T, browser *components.Browser, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, batchCmd := range batch {
				deliverResourceBrowser(t, browser, batchCmd)
			}
			return
		}
		cmd = browser.Update(msg)
	}
}

func TestResourceActionsAreContextual(t *testing.T) {
	screen := &Resources{phase: resourceBrowse, items: []profile.Resource{{ID: "projects", Path: "~/Projects", Strategy: "copy"}}}
	if got := resourceActionIDs(screen.Actions()); !reflect.DeepEqual(got, []string{"strategy", "untrack", "edit", "open", "copy"}) {
		t.Fatalf("tracked actions=%v", got)
	}
	screen.discover = true
	if got := resourceActionIDs(screen.Actions()); len(got) != 0 {
		t.Fatalf("discover actions=%v", got)
	}
}

func TestResourceUntrackedActionsExposeSelectionControls(t *testing.T) {
	screen := &Resources{phase: resourceUntracked, untracked: []string{"settings.json"}}
	actions := screen.Actions()
	if got := resourceActionIDs(actions); !reflect.DeepEqual(got, []string{"select", "select-all", "continue"}) {
		t.Fatalf("actions = %#v", got)
	}
	if actions[0].Shortcut != "space" || actions[2].Shortcut != "enter" {
		t.Fatalf("shortcuts = %#v", actions)
	}
}

func TestResourcesForwardsBrowserChildPreviewResult(t *testing.T) {
	home := t.TempDir()
	child := filepath.Join(home, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "inside.txt"), []byte("preview"), 0o644); err != nil {
		t.Fatal(err)
	}
	browser := components.NewBrowser(components.BrowseResource, components.BrowserConfig{Home: home})
	browser.SetSize(140, 30)
	screen := &Resources{browser: &browser, phase: resourceBrowse}
	deliverResourceScreenBrowser(t, screen, browser.Init())
	if view := screen.View(); !strings.Contains(view, "Next: child") || !strings.Contains(view, "inside.txt") {
		t.Fatalf("child preview was not forwarded:\n%s", view)
	}
}

func deliverResourceScreenBrowser(t *testing.T, screen *Resources, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, batchCmd := range batch {
				deliverResourceScreenBrowser(t, screen, batchCmd)
			}
			return
		}
		cmd = screen.Update(msg)
	}
}

func TestResourceScreenTrackedStatusLabels(t *testing.T) {
	screen := &Resources{items: []profile.Resource{
		{ID: "copy", Path: "~/Copy", Strategy: "copy"},
		{ID: "git", Path: "~/Git", Strategy: "git", Dirty: true},
		{ID: "git-diff", Path: "~/Diff", Strategy: "git+diff"},
	}, git: map[string]resourcesprovider.GitWorkingSummary{"git": {}, "git-diff": {StagedTracked: 1, UnstagedTracked: 2, Untracked: []string{"new.txt", "other.txt"}, SelectedUntracked: []string{"new.txt"}}}, effective: map[string]string{"copy": "/home/user/Copy", "git": "/home/user/Git", "git-diff": "/home/user/Diff"}}
	view := screen.View()
	for _, want := range []string{"Resource", "Strategy", "Portable path", "Effective path", "State", "copy", "~/Copy", "/home/user/Copy", "git", "~/Git", "clean", "git-diff", "~/Diff", "/home/user/Diff", "modified, untracked"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestResourceScreenTrackedTableHidesTrailingColumnsResponsively(t *testing.T) {
	screen := &Resources{width: 40, items: []profile.Resource{{ID: "projects", Path: "~/Projects", Strategy: "copy"}}, effective: map[string]string{"projects": "/home/user/Projects"}}
	view := screen.View()
	for _, want := range []string{"Resource", "Strategy", "Portable path"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow table missing %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"Effective path", "State"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("narrow table retained %q:\n%s", unwanted, view)
		}
	}
}

func TestResourceDetailSeparatesSelectedAndOtherUntracked(t *testing.T) {
	screen := &Resources{items: []profile.Resource{{ID: "dotfiles", Path: "~/Dotfiles", Strategy: "git+diff"}}, git: map[string]resourcesprovider.GitWorkingSummary{"dotfiles": {StagedTracked: 1, UnstagedTracked: 2, Untracked: []string{"selected.txt", "other.txt"}, SelectedUntracked: []string{"selected.txt"}}}, effective: map[string]string{"dotfiles": "/home/user/Dotfiles"}}
	detail := screen.DetailView()
	for _, want := range []string{"Effective path: /home/user/Dotfiles", "Staged: 1", "Unstaged: 2", "Selected untracked: selected.txt", "Other untracked: other.txt"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
	if strings.Contains(detail, "Other untracked: selected.txt") {
		t.Fatalf("selected untracked file was repeated as other:\n%s", detail)
	}
}

func TestResourceTabReturnsFromActiveDiscoverBrowser(t *testing.T) {
	browser := components.NewBrowser(components.BrowseResource, components.BrowserConfig{Home: t.TempDir()})
	screen := &Resources{phase: resourceBrowse, discover: true, browser: &browser}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if screen.discover || screen.browser != nil {
		t.Fatalf("discover=%t browser=%v", screen.discover, screen.browser)
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
	if screen.selected != len(items)-1 || !strings.Contains(screen.View(), "resource-7") {
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
	screen := &Resources{items: []profile.Resource{{ID: "projects", Path: "~/Projects"}}}
	request, ok := screen.Update(tea.KeyPressMsg{Code: 'u'})().(components.ModalRequest)
	if !ok || !strings.Contains(request.Content, "live path remains untouched") {
		t.Fatalf("request=%#v", request)
	}
	if view := screen.View(); !strings.Contains(view, "projects") || strings.Contains(view, "live path remains untouched") {
		t.Fatalf("confirmation did not preserve tracked background: %q", view)
	}
}

func resourceActionIDs(actions []ResourceAction) []string {
	ids := make([]string, len(actions))
	for i, action := range actions {
		ids[i] = action.ID
	}
	return ids
}
