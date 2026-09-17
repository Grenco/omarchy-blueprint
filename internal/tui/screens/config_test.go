package screens

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/providers/config"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestConfigPresentationForEveryClassification(t *testing.T) {
	tests := map[config.Classification]configPresentation{
		config.ConfigAmbiguousBaseline:  {configNeedsReview, "Needs review", "This file differs, but Blueprint cannot safely tell whether it is your change or an Omarchy-version difference."},
		config.ConfigAmbiguousDeletion:  {configNeedsReview, "Removal needs review", "An Omarchy default is missing, but Blueprint cannot safely tell whether you intentionally removed it."},
		config.ConfigModifiedBaseline:   {configChanges, "Changed from Omarchy default", "You changed an Omarchy-provided configuration file. Blueprint can remember the change."},
		config.ConfigDeletedBaseline:    {configChanges, "Omarchy default removed", "An Omarchy-provided file is absent and Blueprint has enough information to treat that removal as intentional."},
		config.ConfigAdded:              {configChanges, "Added configuration", "This configuration file exists on your system but is not part of Omarchy's defaults."},
		config.ConfigUnchangedBaseline:  {configNoActionNeeded, "Matches Omarchy default", "This file is unchanged from Omarchy, so there is nothing to save."},
		config.ConfigHistoricalBaseline: {configNoActionNeeded, "Matches a previous Omarchy default", "This looks like an older Omarchy version rather than a personal customisation. Blueprint leaves it alone unless you explicitly include it."},
		config.ConfigDelegated:          {configNotManaged, "Handled elsewhere", "Another Blueprint feature owns this path, so Config will not capture it again."},
		config.ConfigExcluded:           {configNotManaged, "Excluded", "You told Config to leave this path alone."},
		config.ConfigVolatile:           {configNotManaged, "Not suitable for capture", "This looks like application/runtime state rather than portable configuration."},
		config.ConfigSensitive:          {configNotManaged, "Sensitive", "Blueprint detected content that should not be saved into the profile."},
		config.ConfigOversized:          {configNotManaged, "Too large", "This file exceeds Config's safe automatic capture limit."},
		config.ConfigUnsupported:        {configNotManaged, "Unsupported", "This filesystem entry cannot be handled safely by Config."},
		config.ConfigUnmanagedSymlink:   {configNotManaged, "Symlink not managed here", "This path is a symbolic link that Config deliberately does not own."},
	}
	for classification, want := range tests {
		if got := configPresentationFor(classification); got != want {
			t.Errorf("configPresentationFor(%q) = %#v, want %#v", classification, got, want)
		}
	}
}

func TestConfigScreenRendersBroadGroupsWithUserFacingStateAndPolicy(t *testing.T) {
	screen := &Config{width: 120, candidates: []config.Candidate{{Path: ".config/gh/hosts.yml", Classification: config.ConfigSensitive, Reason: "sensitive"}, {Path: ".config/nvim/init.lua", Classification: config.ConfigModifiedBaseline, Reason: "modified-baseline"}}}
	view := screen.View()
	for _, want := range []string{"Path", "State", "Policy", "Changes", "Not managed by Config", ".config/nvim/init.lua", "Auto"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "modified-baseline") {
		t.Fatalf("raw provider reason leaked into table: %q", view)
	}
}

func TestConfigScreenGroupsAndDefaultCollapse(t *testing.T) {
	screen := &Config{width: 80, candidates: []config.Candidate{{Path: ".config/review", Classification: config.ConfigAmbiguousBaseline}, {Path: ".config/a", Classification: config.ConfigAdded, Reason: "added"}, {Path: ".config/b", Classification: config.ConfigAdded, Reason: "added"}, {Path: ".config/unchanged", Classification: config.ConfigUnchangedBaseline}, {Path: ".config/c", Classification: config.ConfigSensitive, Reason: "sensitive"}}}
	view := screen.View()
	for _, want := range []string{"Needs review", "Changes", "No action needed", "Not managed by Config"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing group %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "Needs review") > strings.Index(view, "Changes") || strings.Index(view, "Changes") > strings.Index(view, "No action needed") || strings.Index(view, "No action needed") > strings.Index(view, "Not managed by Config") {
		t.Fatalf("groups are out of order:\n%s", view)
	}
	if strings.Contains(view, ".config/unchanged") || strings.Contains(view, ".config/c") {
		t.Fatalf("default-collapsed candidates are visible:\n%s", view)
	}
	screen.selected = 2 // Changes group heading.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view = screen.View()
	if !strings.Contains(view, "▶ Changes") || strings.Contains(view, ".config/a") || strings.Contains(view, ".config/c") || !strings.Contains(view, "▶ Not managed by Config") {
		t.Fatalf("collapsed view=%q", view)
	}
}

func TestConfigScreenRowsFilterAndViewportKeepSelectedCandidateVisible(t *testing.T) {
	candidates := make([]config.Candidate, 6)
	for i := range candidates {
		candidates[i] = config.Candidate{Path: ".config/item-" + string(rune('a'+i)), Classification: config.ConfigAdded}
	}
	screen := &Config{width: 80, height: 4, candidates: candidates}
	// group row plus six candidates; move to the final candidate.
	for range candidates {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if candidate := screen.selectedCandidate(); candidate.Path != ".config/item-f" {
		t.Fatalf("selected candidate=%#v", candidate)
	}
	view := screen.View()
	if !strings.Contains(view, ".config/item-f") || strings.Contains(view, ".config/item-a") {
		t.Fatalf("viewport wrong: %q", view)
	}
	screen.Update(tea.KeyPressMsg{Code: '/'})
	screen.Update(tea.KeyPressMsg{Code: '-'})
	screen.Update(tea.KeyPressMsg{Code: 'f'})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(screen.View(), ".config/item-f") {
		t.Fatalf("filter did not retain matching group and row: %q", screen.View())
	}
}

func TestConfigGroupNavigationPreservesCollapsedGroupsAndFilterInput(t *testing.T) {
	screen := &Config{candidates: []config.Candidate{
		{Path: ".config/review", Classification: config.ConfigAmbiguousBaseline},
		{Path: ".config/changed", Classification: config.ConfigAdded},
		{Path: ".config/sensitive", Classification: config.ConfigSensitive},
	}}
	screen.selected = 1 // Child of the first group.
	screen.Update(tea.KeyPressMsg{Code: '['})
	if screen.selected != 0 {
		t.Fatalf("first [ selected %d, want first group heading", screen.selected)
	}
	screen.Update(tea.KeyPressMsg{Code: ']'})
	if screen.selected != 2 {
		t.Fatalf("] selected %d, want next group heading", screen.selected)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !screen.collapsed[configChanges] {
		t.Fatal("enter did not collapse selected group")
	}
	screen.Update(tea.KeyPressMsg{Code: ']'})
	if screen.selected != 3 || !screen.collapsed[configChanges] {
		t.Fatalf("jump changed collapsed state or selected %d", screen.selected)
	}
	screen.Update(tea.KeyPressMsg{Code: '/'})
	screen.Update(tea.KeyPressMsg{Code: '['})
	screen.Update(tea.KeyPressMsg{Code: ']'})
	if screen.filter != "[]" {
		t.Fatalf("filter lost bracket input: %q", screen.filter)
	}
}

func TestConfigScreenIgnoresStaleInspection(t *testing.T) {
	screen := &Config{inspectionID: 2, selected: 1, candidates: []config.Candidate{{Path: ".config/current", Classification: config.ConfigAdded}}}
	screen.Update(configInspectionMsg{requestID: 1, inspection: workflow.ConfigInspection{Candidate: config.Candidate{Path: ".config/current"}, LivePath: "stale"}})
	if screen.inspection.LivePath != "" {
		t.Fatalf("stale inspection applied: %#v", screen.inspection)
	}
	screen.Update(configInspectionMsg{requestID: 2, inspection: workflow.ConfigInspection{Candidate: config.Candidate{Path: ".config/current"}, LivePath: "current"}})
	if screen.inspection.LivePath != "current" {
		t.Fatalf("current inspection was not applied: %#v", screen.inspection)
	}
}

func TestConfigScreenDetailUsesPlainLanguage(t *testing.T) {
	live := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(live, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	screen := &Config{selected: 1, candidates: []config.Candidate{{Path: ".config/tool/config", Classification: config.ConfigModifiedBaseline, Reason: "modified-baseline"}}, inspection: workflow.ConfigInspection{Candidate: config.Candidate{Path: ".config/tool/config"}, LivePath: live, LiveHandoffSafe: true, BaselinePath: "/baseline/config", ProfilePath: "/profile/config", Managed: true}}
	detail := screen.DetailView()
	for _, want := range []string{"State: Changed from Omarchy default", "Policy: Auto", "Policy meaning: Blueprint decides based on Omarchy defaults, ownership, and safety rules.", "Meaning: You changed an Omarchy-provided configuration file. Blueprint can remember the change.", "Saved in profile: Yes", "Live file: " + live, "Omarchy default: /baseline/config", "Saved copy: /profile/config"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
	for _, unwanted := range []string{"Baseline:", "Provider reason:", "Effective policy:", "Available actions:"} {
		if strings.Contains(detail, unwanted) {
			t.Fatalf("detail contains obsolete %q:\n%s", unwanted, detail)
		}
	}
	session, _ := newSyncSession(t)
	if err := session.SetConfigPolicy(context.Background(), ".config/tool/config", "include"); err != nil {
		t.Fatal(err)
	}
	screen.session = session
	detail = screen.DetailView()
	for _, want := range []string{"Policy: Included", "Policy meaning: You asked Config to manage this path when it is safe to do so.", "Included never overrides safety rules."} {
		if !strings.Contains(detail, want) {
			t.Fatalf("included detail missing %q:\n%s", want, detail)
		}
	}
}

func TestConfigFocusRevealsFilteredCollapsedTarget(t *testing.T) {
	screen := &Config{filter: "other", filtering: true, collapsed: defaultConfigCollapsed(), candidates: []config.Candidate{{Path: ".config/target", Classification: config.ConfigAdded}}}
	screen.Focus(".config/target")
	if screen.filter != "" || screen.filtering || screen.collapsed[configChanges] {
		t.Fatalf("focus did not reveal target: filter=%q filtering=%v collapsed=%v", screen.filter, screen.filtering, screen.collapsed)
	}
}

func TestConfigHandoffEligibilityUsesInspectionState(t *testing.T) {
	screen := &Config{selected: 1, candidates: []config.Candidate{{Path: ".config/current", Classification: config.ConfigAdded}}, inspection: workflow.ConfigInspection{Candidate: config.Candidate{Path: ".config/current"}, LivePath: filepath.Join(t.TempDir(), "missing"), LiveHandoffSafe: true}}
	if !screen.CanHandoff() {
		t.Fatal("inspection marked handoff-safe was rejected")
	}
}

func TestConfigHandoffInvalidatesWhenSelectionMovesToHeading(t *testing.T) {
	screen := &Config{selected: 1, candidates: []config.Candidate{{Path: ".config/a", Classification: config.ConfigAdded}}, inspection: workflow.ConfigInspection{Candidate: config.Candidate{Path: ".config/a"}, LivePath: "/live/a", LiveHandoffSafe: true}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if screen.inspection.LivePath != "" || screen.CanHandoff() || screen.Update(tea.KeyPressMsg{Code: 'o'}) != nil {
		t.Fatalf("stale handoff remained available: %#v", screen.inspection)
	}
}

func TestConfigIgnoresLateInspectionAfterSelectionChanges(t *testing.T) {
	screen := &Config{selected: 1, candidates: []config.Candidate{{Path: ".config/a", Classification: config.ConfigAdded}, {Path: ".config/b", Classification: config.ConfigAdded}}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	requestID := screen.inspectionID
	screen.Update(configInspectionMsg{requestID: requestID, inspection: workflow.ConfigInspection{Candidate: config.Candidate{Path: ".config/a"}, LivePath: "/live/a", LiveHandoffSafe: true}})
	if screen.inspection.LivePath != "" || screen.CanHandoff() {
		t.Fatalf("late inspection applied to %q: %#v", screen.selectedCandidate().Path, screen.inspection)
	}
}

func TestConfigDiffHeaderSanitizesLabels(t *testing.T) {
	document := inspection.DiffDocument{OldLabel: "old\nlabel", NewLabel: "new\x1b"}
	viewer := components.NewDiffViewer(document)
	screen := &Config{diff: &viewer, inspection: workflow.ConfigInspection{BaselineToLive: &document}}
	view := screen.View()
	if strings.Contains(view, "\x1b") || !strings.Contains(view, "old?label <-> new?") {
		t.Fatalf("unsafe diff header=%q", view)
	}
}

func TestConfigPolicyModalSanitizesPath(t *testing.T) {
	screen := &Config{confirm: "include", selected: 1, candidates: []config.Candidate{{Path: ".config/bad\nname\x1b", Classification: config.ConfigAdded}}}
	request := screen.confirmModal()().(components.ModalRequest)
	if strings.Contains(request.Content, "\x1b") || !strings.Contains(request.Content, ".config/bad?name?") {
		t.Fatalf("unsafe modal=%q", request.Content)
	}
}

func TestConfigScreenRendersPersistedAndInheritedPolicy(t *testing.T) {
	session, _ := newSyncSession(t)
	if err := session.SetConfigPolicy(context.Background(), ".config/excluded", "exclude"); err != nil {
		t.Fatal(err)
	}
	if err := session.SetConfigPolicy(context.Background(), ".config/included.conf", "include"); err != nil {
		t.Fatal(err)
	}
	screen := &Config{session: session, width: 100, candidates: []config.Candidate{
		{Path: ".config/excluded/child.conf", Classification: config.ConfigAdded},
		{Path: ".config/included.conf", Classification: config.ConfigAdded},
	}}
	view := screen.View()
	for _, want := range []string{"Excluded", "Included"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing persisted policy %q:\n%s", want, view)
		}
	}
	screen.selected = 1
	if detail := screen.DetailView(); !strings.Contains(detail, "Policy: Excluded") {
		t.Fatalf("detail missing inherited exclusion:\n%s", detail)
	}
}
