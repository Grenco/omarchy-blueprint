package screens

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestResourcesEmptyTrackedViewExplainsOptionalPurpose(t *testing.T) {
	screen := &Resources{
		width: 80,
		phase: resourceBrowse,
		items: nil,
	}

	view := screen.View()
	for _, want := range []string{
		"No extra resources tracked",
		"files, folders, or Git projects",
		"do not fit one of its normal categories",
		"Exact never deletes Resource data.",
		"Press d to Discover one.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("empty Resources missing %q:\n%s", want, view)
		}
	}
}

func TestResourcesExposePerIDCaptureAndRestorePolicy(t *testing.T) {
	screen := &Resources{
		width: 80, phase: resourceBrowse, tab: "Restore",
		targets: []workflow.TargetInspection{{Key: "resource:projects", Label: "projects", Desired: workflow.TargetPresent, Current: workflow.TargetAbsent, CaptureEligible: true, RestoreEligible: true, Capabilities: workflow.TargetCapabilities{SupportsCapture: true, SupportsRestore: true}}},
		policies: map[string]policy.Effective{"resource:projects": {
			Capture: policy.EffectiveSetting{Enabled: true, Source: policy.Source{Kind: policy.SourceDefault}},
			Restore: policy.EffectiveSetting{Enabled: false, Explicit: true, Source: policy.Source{Kind: policy.SourceProfileTarget}},
		}},
	}
	view := screen.View()
	for _, want := range []string{"State", "Capture", "Restore", "Profile defaults", "projects", "Skip", "Exact never deletes Resource data"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Resources policy view missing %q:\n%s", want, view)
		}
	}
	if detail := screen.DetailView(); !strings.Contains(detail, "Capture policy: Include (inherited)") || !strings.Contains(detail, "Capture source: default") || !strings.Contains(detail, "Restore policy: Skip (explicit)") || !strings.Contains(detail, "Restore source: profile-target") || !strings.Contains(detail, "Supports restore: true") {
		t.Fatalf("Resources policy details incomplete:\n%s", detail)
	}
}

func TestResourcesUseStateAndPolicyTablesAtResponsiveWidths(t *testing.T) {
	for _, width := range []int{140, 100, 80} {
		state := &Resources{
			width: width, phase: resourceBrowse, tab: "State",
			items:   []profile.Resource{{ID: "projects", Path: "~/Projects", Strategy: "copy"}},
			targets: []workflow.TargetInspection{{Key: "resource:projects", Label: "projects", Desired: workflow.TargetAbsent, Current: workflow.TargetPresent, CaptureEligible: true, RestoreEligible: true}},
		}
		view := state.View()
		for _, want := range []string{"RESOURCE", "DESIRED", "projects", "Absent", "Desired absent", "Exact never deletes Resource data"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%d-column Resource State missing %q:\n%s", width, want, view)
			}
		}

		policyScreen := &Resources{
			width: width, phase: resourceBrowse, tab: "Restore",
			targets:  []workflow.TargetInspection{{Key: "resource:projects", Label: "projects", Desired: workflow.TargetAbsent, Current: workflow.TargetPresent, RestoreEligible: true}},
			policies: map[string]policy.Effective{"resource:projects": {Restore: policy.EffectiveSetting{Enabled: false, Explicit: true, Source: policy.Source{Kind: policy.SourceMachineTarget}}}},
		}
		view = policyScreen.View()
		for _, want := range []string{"RESOURCE", "RESTORE", "projects", "Skip", "Exact never deletes Resource data"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%d-column Resource Restore missing %q:\n%s", width, want, view)
			}
		}
		if width >= 90 && (!strings.Contains(view, "SOURCE") || !strings.Contains(view, "This machine")) {
			t.Fatalf("%d-column Resource Restore lost concise source:\n%s", width, view)
		}
		if strings.Contains(view, "projects —") {
			t.Fatalf("%d-column Resource Restore regressed to prose:\n%s", width, view)
		}
	}
}

func TestResourcesDiscoverDoesNotShowTrackedEmptyGuidance(t *testing.T) {
	screen := &Resources{
		width:    80,
		phase:    resourceBrowse,
		discover: true,
	}

	view := screen.View()
	if strings.Contains(view, "No extra resources tracked") {
		t.Fatalf("Tracked empty-state leaked into Discover:\n%s", view)
	}
	if !strings.Contains(view, "Discover") {
		t.Fatalf("Discover tab missing:\n%s", view)
	}
}

func TestResourceScreenBrowseStrategyStartsOneTrack(t *testing.T) {
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
	if screen.phase != resourceStrategy || screen.candidate.Path == "" || screen.browser != nil || screen.candidateInspection.Path != screen.candidate.Path {
		t.Fatalf("candidate did not open strategy picker: phase=%q candidate=%#v inspection=%#v", screen.phase, screen.candidate, screen.candidateInspection)
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
	if got := resourceActionIDs(screen.Actions()); !reflect.DeepEqual(got, []string{"tab", "discover", "strategy", "untrack", "edit", "open", "copy"}) {
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
	}, targets: []workflow.TargetInspection{
		{Key: "resource:copy", Label: "copy", Desired: workflow.TargetPresent, Current: workflow.TargetPresent},
		{Key: "resource:git", Label: "git", Desired: workflow.TargetPresent, Current: workflow.TargetPresent},
		{Key: "resource:git-diff", Label: "git-diff", Desired: workflow.TargetPresent, Current: workflow.TargetAbsent},
	}, git: map[string]resourcesprovider.GitWorkingSummary{"git": {}, "git-diff": {StagedTracked: 1, UnstagedTracked: 2, Untracked: []string{"new.txt", "other.txt"}, SelectedUntracked: []string{"new.txt"}}}, effective: map[string]string{"copy": "/home/user/Copy", "git": "/home/user/Git", "git-diff": "/home/user/Diff"}}
	view := screen.View()
	for _, want := range []string{"RESOURCE", "DESIRED", "CURRENT", "STATUS", "copy", "Present", "In sync", "git", "git-diff", "Absent", "Missing"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if detail := screen.DetailView(); !strings.Contains(detail, "Strategy: copy") || !strings.Contains(detail, "Effective path: /home/user/Copy") {
		t.Fatalf("tracking detail did not remain in Details:\n%s", detail)
	}
}

func TestResourceScreenTrackedTableHidesTrailingColumnsResponsively(t *testing.T) {
	screen := &Resources{width: 40, items: []profile.Resource{{ID: "projects", Path: "~/Projects", Strategy: "copy"}}, effective: map[string]string{"projects": "/home/user/Projects"}}
	view := screen.View()
	for _, want := range []string{"RESOURCE", "DESIRED", "STATUS", "projects", "Present", "Not checked"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow table missing %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"CURRENT", "Strategy", "Portable path", "Effective path"} {
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
	if screen.phase != resourcePending {
		t.Fatalf("selector did not apply selection: phase=%q", screen.phase)
	}
}

func TestResourceEditorEscapeLeavesTrackedSelectionUnchanged(t *testing.T) {
	screen := &Resources{phase: resourceStrategy, strategy: "copy", selected: 1, editingResourceID: "second", items: []profile.Resource{{ID: "first"}, {ID: "second"}}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if screen.phase != resourceBrowse || screen.selected != 1 || screen.editingResourceID != "" {
		t.Fatalf("cancelled editor: phase=%q selected=%d editing=%q", screen.phase, screen.selected, screen.editingResourceID)
	}
}

func TestResourceGitDiffEscapeReturnsToStrategy(t *testing.T) {
	screen := &Resources{phase: resourceUntracked, untracked: []string{"chosen.txt"}, chosen: map[string]bool{}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if screen.phase != resourceStrategy {
		t.Fatalf("cancelled git+diff: phase=%q", screen.phase)
	}
}

func TestResourceStrategyPickerUsesNavigationAndGitCapability(t *testing.T) {
	screen := &Resources{phase: resourceStrategy, strategy: "copy", candidateInspection: workflow.PathInspection{Git: &workflow.GitInspection{}}, untracked: []string{"new.txt"}}
	screen.selectStrategy("copy")
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if screen.strategy != "git" || !strings.Contains(screen.View(), "> git:") {
		t.Fatalf("strategy picker did not navigate: strategy=%q view=%q", screen.strategy, screen.View())
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if screen.strategy != "git+diff" || screen.phase != resourceUntracked {
		t.Fatalf("git+diff did not open selector: strategy=%q phase=%q", screen.strategy, screen.phase)
	}
}

func TestResourceGitDiffWithoutUntrackedAppliesDirectly(t *testing.T) {
	screen := &Resources{phase: resourceStrategy, strategy: "git+diff", candidateInspection: workflow.PathInspection{Git: &workflow.GitInspection{}}}
	screen.selectStrategy("git+diff")
	if cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil || screen.phase != resourcePending {
		t.Fatalf("empty git+diff did not apply: phase=%q cmd=%v", screen.phase, cmd != nil)
	}
}

func TestResourceInspectionFailureUsesInspectionMessage(t *testing.T) {
	screen := &Resources{requestID: 2}
	screen.Update(resourceExistingInspectMsg{requestID: 2, err: fmt.Errorf("missing origin")})
	if screen.phase != resourceInspectError || !strings.Contains(screen.View(), "Unable to inspect resource: missing origin") {
		t.Fatalf("inspection failure=%q phase=%q", screen.View(), screen.phase)
	}
}

func TestResourceTrackRequestKeepsEditedIdentityAndDiffSelections(t *testing.T) {
	screen := &Resources{selected: 0, items: []profile.Resource{{ID: "first"}, {ID: "second"}}, editingResourceID: "second", candidate: components.BrowserEntry{Path: "/work/second"}, strategy: "git+diff", untracked: []string{"kept.txt", "removed.txt", "added.txt"}, chosen: map[string]bool{"kept.txt": true, "added.txt": true}}
	request := screen.trackRequest()
	if request.ID != "second" || !reflect.DeepEqual(request.IncludeUntracked, []string{"kept.txt", "added.txt"}) || !reflect.DeepEqual(request.ExcludeUntracked, []string{"removed.txt"}) {
		t.Fatalf("request=%#v", request)
	}
	screen.strategy = "git"
	request = screen.trackRequest()
	if len(request.IncludeUntracked) != 0 || len(request.ExcludeUntracked) != 0 {
		t.Fatalf("git request retained diff state: %#v", request)
	}
}

func TestExistingResourceGitDiffSelectorUsesCurrentAndSavedUntracked(t *testing.T) {
	screen := &Resources{selected: 1, items: []profile.Resource{{ID: "first"}, {ID: "second", Strategy: "git+diff", Untracked: []profile.GitUntrackedFile{{Path: "current.txt"}, {Path: "old-local.env"}}}}, requestID: 3}
	screen.Update(resourceExistingInspectMsg{requestID: 3, item: screen.items[1], inspection: workflow.ResourceInspection{EffectivePath: "/work/second", Git: &resourcesprovider.GitWorkingSummary{Untracked: []string{"current.txt"}}}})
	if screen.selected != 1 || screen.editingResourceID != "second" || !screen.chosen["current.txt"] || screen.phase != resourceStrategy || len(screen.strategies()) != 3 || !reflect.DeepEqual(screen.untracked, []string{"current.txt", "old-local.env"}) {
		t.Fatalf("editor state=%#v selected=%d editing=%q chosen=%#v untracked=%#v phase=%q", screen.candidate, screen.selected, screen.editingResourceID, screen.chosen, screen.untracked, screen.phase)
	}
	if cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || screen.phase != resourceUntracked || !strings.Contains(screen.View(), "old-local.env  ignored now") {
		t.Fatalf("git+diff selector=%q phase=%q cmd=%v", screen.View(), screen.phase, cmd != nil)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	screen.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	request := screen.trackRequest()
	if !reflect.DeepEqual(request.ExcludeUntracked, []string{"old-local.env"}) {
		t.Fatalf("request did not exclude stale selection: %#v", request)
	}
}

func TestResourceEditCompletionRefocusesEditedResource(t *testing.T) {
	screen := &Resources{requestID: 4, editingResourceID: "second"}
	if cmd := screen.Update(resourceTrackedMsg{requestID: 4}); cmd == nil || screen.focusID != "second" || screen.editingResourceID != "" {
		t.Fatalf("completion did not retain focus: focus=%q editing=%q cmd=%v", screen.focusID, screen.editingResourceID, cmd != nil)
	}
}

func TestResourceUntrackConfirmationRestoresBrowseAndCompletionStaysTracked(t *testing.T) {
	screen := &Resources{phase: resourceBrowse, items: []profile.Resource{{ID: "projects", Path: "~/Projects"}}}
	screen.Update(tea.KeyPressMsg{Code: 'u'})
	if screen.phase != resourceBrowse || screen.confirmFrom != resourceBrowse || screen.confirm != "untrack" {
		t.Fatalf("confirmation state: phase=%q from=%q confirm=%q", screen.phase, screen.confirmFrom, screen.confirm)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if screen.phase != resourceBrowse || screen.confirm != "" {
		t.Fatalf("cancelled untrack: phase=%q confirm=%q", screen.phase, screen.confirm)
	}
	screen.phase, screen.confirm = resourceConfirm, "untrack"
	screen.Update(resourceUntrackedMsg{})
	if screen.phase != resourceBrowse {
		t.Fatalf("completed untrack phase=%q", screen.phase)
	}
}

func TestResourceCandidateSnapshotIgnoresLateBrowserUpdates(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "candidate"), 0o755); err != nil {
		t.Fatal(err)
	}
	browser := components.NewBrowser(components.BrowseResource, components.BrowserConfig{Home: home, InspectPathCmd: func(id uint64, path string) tea.Cmd {
		return func() tea.Msg {
			return components.BrowserInspectionMsg{RequestID: id, Inspection: workflow.PathInspection{Path: path, SuggestedStrategy: "git+diff", Git: &workflow.GitInspection{UntrackedPaths: []string{"chosen.txt"}}}}
		}
	}})
	deliverResourceBrowser(t, &browser, browser.Init())
	screen := &Resources{browser: &browser, phase: resourceBrowse}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen.chosen["chosen.txt"] = true
	screen.untrackedCursor = 0
	screen.Update(components.BrowserReadDirMsg{})
	screen.Update(components.BrowserChildReadDirMsg{})
	screen.Update(components.BrowserInspectionMsg{})
	if !screen.chosen["chosen.txt"] || screen.untrackedCursor != 0 || !reflect.DeepEqual(screen.untracked, []string{"chosen.txt"}) {
		t.Fatalf("late browser update reset selection: chosen=%#v cursor=%d untracked=%#v", screen.chosen, screen.untrackedCursor, screen.untracked)
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

func TestResourceUntrackModalSanitizesID(t *testing.T) {
	screen := &Resources{items: []profile.Resource{{ID: "project\nname\x1b"}}}
	request := screen.Update(tea.KeyPressMsg{Code: 'u'})().(components.ModalRequest)
	if strings.Contains(request.Content, "\x1b") || !strings.Contains(request.Content, "project?name?") {
		t.Fatalf("unsafe modal=%q", request.Content)
	}
}

func TestResourcesScreenIgnoresStaleStatus(t *testing.T) {
	screen := &Resources{requestID: 2}
	screen.Update(resourcesStatusMsg{requestID: 1, items: []profile.Resource{{ID: "stale"}}})
	if len(screen.items) != 0 {
		t.Fatalf("stale status changed items: %#v", screen.items)
	}
}

func TestResourcesHandoffUsesCachedEffectivePath(t *testing.T) {
	screen := &Resources{items: []profile.Resource{{ID: "notes", Kind: "directory"}}, effective: map[string]string{"notes": "/effective/notes"}}
	cmd := screen.Update(tea.KeyPressMsg{Code: 'o'})
	request, ok := cmd().(HandoffRequest)
	if !ok || request.Path != "/effective/notes" || !request.IsDir {
		t.Fatalf("handoff=%#v", request)
	}
}

func TestResourcesSanitizesUntrackedPath(t *testing.T) {
	screen := &Resources{phase: resourceUntracked, untracked: []string{"line\nnext\x1b"}, chosen: map[string]bool{}}
	view := screen.View()
	if strings.ContainsAny(view, "\x1b\r\t") || strings.Count(view, "\n") != 1 || !strings.Contains(view, "line?next?") {
		t.Fatalf("unsafe resource view=%q", view)
	}
}

func resourceActionIDs(actions []ResourceAction) []string {
	ids := make([]string, len(actions))
	for i, action := range actions {
		ids[i] = action.ID
	}
	return ids
}
