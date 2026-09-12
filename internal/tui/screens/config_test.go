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

func TestConfigScreenShowsProviderClassificationAndExactReason(t *testing.T) {
	screen := &Config{width: 80, candidates: []config.Candidate{
		{Path: ".config/gh/hosts.yml", Classification: config.ConfigSensitive, Reason: string(config.PolicySensitive)},
		{Path: ".config/nvim/init.lua", Classification: config.ConfigModifiedBaseline, Reason: "modified-baseline"},
	}}
	view := screen.View()
	if !strings.Contains(view, "> .config/gh/hosts.yml  sensitive (sensitive)") || !strings.Contains(view, "Reason: sensitive") {
		t.Fatalf("view=%q", view)
	}
}

func TestConfigScreenCollapsesSelectedClassificationGroup(t *testing.T) {
	screen := &Config{width: 80, candidates: []config.Candidate{
		{Path: ".config/a", Classification: config.ConfigAdded, Reason: "added"},
		{Path: ".config/b", Classification: config.ConfigAdded, Reason: "added"},
		{Path: ".config/c", Classification: config.ConfigSensitive, Reason: "sensitive"},
	}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := screen.View()
	if !strings.Contains(view, "[+ added]") || strings.Contains(view, ".config/a") || !strings.Contains(view, ".config/c") {
		t.Fatalf("collapsed view=%q", view)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view = screen.View(); !strings.Contains(view, "[- added]") || !strings.Contains(view, ".config/a") {
		t.Fatalf("expanded view=%q", view)
	}
}

func TestConfigScreenDownSelectsNextCandidate(t *testing.T) {
	screen := &Config{candidates: []config.Candidate{{Path: ".config/first"}, {Path: ".config/second"}}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if candidate := screen.selectedCandidate(); candidate.Path != ".config/second" {
		t.Fatalf("selected candidate=%#v", candidate)
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

func TestConfigScreenDetailShowsInspectionPolicyAndActionValidity(t *testing.T) {
	live := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(live, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	screen := &Config{candidates: []config.Candidate{{Path: ".config/tool/config", Classification: config.ConfigModifiedBaseline, Reason: "modified-baseline"}}, inspection: workflow.ConfigInspection{LivePath: live, BaselinePath: "/baseline/config", ProfilePath: "/profile/config", Managed: true}}
	detail := screen.DetailView()
	for _, want := range []string{"Classification: modified-baseline", "Reason: modified-baseline", "Live: " + live, "Effective policy: managed", "Edit/open/copy: available"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
}
