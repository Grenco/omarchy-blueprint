package screens

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/providers/config"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestConfigScreenRendersFlattenedTableWithUserFacingStateAndPolicy(t *testing.T) {
	screen := &Config{width: 80, candidates: []config.Candidate{{Path: ".config/gh/hosts.yml", Classification: config.ConfigSensitive, Reason: "sensitive"}, {Path: ".config/nvim/init.lua", Classification: config.ConfigModifiedBaseline, Reason: "modified-baseline"}}}
	view := screen.View()
	for _, want := range []string{"Path", "Policy", "Changed from baseline", "Sensitive", ".config/nvim/init.lua", "managed", "not eligible"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "modified-baseline") {
		t.Fatalf("raw provider reason leaked into table: %q", view)
	}
}

func TestConfigScreenCollapsesSelectedGroupAndKeepsOtherGroups(t *testing.T) {
	screen := &Config{width: 80, candidates: []config.Candidate{{Path: ".config/a", Classification: config.ConfigAdded, Reason: "added"}, {Path: ".config/b", Classification: config.ConfigAdded, Reason: "added"}, {Path: ".config/c", Classification: config.ConfigSensitive, Reason: "sensitive"}}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := screen.View()
	if !strings.Contains(view, "▶ New configuration") || strings.Contains(view, ".config/a") || !strings.Contains(view, ".config/c") {
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

func TestConfigScreenIgnoresStaleInspection(t *testing.T) {
	screen := &Config{inspectionID: 2, candidates: []config.Candidate{{Path: ".config/current"}}}
	screen.Update(configInspectionMsg{requestID: 1, inspection: workflow.ConfigInspection{LivePath: "stale"}})
	if screen.inspection.LivePath != "" {
		t.Fatalf("stale inspection applied: %#v", screen.inspection)
	}
	screen.Update(configInspectionMsg{requestID: 2, inspection: workflow.ConfigInspection{LivePath: "current"}})
	if screen.inspection.LivePath != "current" {
		t.Fatalf("current inspection was not applied: %#v", screen.inspection)
	}
}

func TestConfigScreenDetailExplainsWhyAndKeepsRawReasonSecondary(t *testing.T) {
	live := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(live, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	screen := &Config{selected: 1, candidates: []config.Candidate{{Path: ".config/tool/config", Classification: config.ConfigModifiedBaseline, Reason: "modified-baseline"}}, inspection: workflow.ConfigInspection{LivePath: live, BaselinePath: "/baseline/config", ProfilePath: "/profile/config", Managed: true}}
	detail := screen.DetailView()
	for _, want := range []string{"State: Changed from baseline", "Policy: managed", "Why: modified baseline", "Provider reason: modified-baseline", "Live: " + live, "Effective policy: managed", "Edit/open/copy: available"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
}
