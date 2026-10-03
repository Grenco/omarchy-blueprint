package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// freshMachineRestorePlan is what a fresh Omarchy machine plans: every saved
// package blocked on missing sync databases, and a service whose program
// those packages would install.
func freshMachineRestorePlan(packages int) model.RestorePlan {
	pkgs := model.CompatibilityCategory{Category: "packages", Applies: true, State: model.CompatibilityUnknown, Authority: model.CompatibilityBlocked}
	var operations []string
	for i := range packages {
		pkgs.Findings = append(pkgs.Findings, model.CompatibilityFinding{Code: "packages.origin.unknown", Target: fmt.Sprintf("official:pkg-%03d", i), State: model.CompatibilityUnknown, Authority: model.CompatibilityBlocked, Summary: "pacman's package databases aren't set up yet", RequirementID: "packages.metadata"})
		operations = append(operations, fmt.Sprintf("packages.install.%03d", i))
	}
	return model.RestorePlan{
		Compatibility: model.CompatibilityReport{Categories: []model.CompatibilityCategory{
			pkgs,
			{Category: "services", Applies: true, State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Findings: []model.CompatibilityFinding{{Code: "services.unit.invalid", Target: "waybar-extra.service", State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Summary: "systemd rejected this service: Command /usr/bin/waybar-extra is not executable"}}},
			{Category: "themes", Applies: true, State: model.CompatibilitySupported, Authority: model.CompatibilityUnchanged, Evidence: []model.CompatibilityEvidence{{Kind: "builtin", Summary: "built-in theme"}}},
		}},
		Requirements: []model.Requirement{{ID: "packages.metadata", Provider: "packages", Reason: "pacman's package databases aren't set up on this machine yet", Remediation: []string{"omarchy", "update"}, Operations: operations}},
		Operations:   []model.Operation{{Provider: "themes", Resource: "theme:tokyo-night", Action: "install"}},
	}
}

func TestRenderPlanSummarizesHundredsOfBlockedTargetsWithNextSteps(t *testing.T) {
	view := renderPlanWithOptions(freshMachineRestorePlan(200), true, restorePlanOptions{})
	if lines := strings.Count(view, "\n"); lines > 30 {
		t.Fatalf("blocked plan is %d lines; blockers must be summarized, not enumerated:\n%s", lines, view)
	}
	for _, want := range []string{
		"200 targets (official:pkg-000, official:pkg-001, … +198 more)",
		"Needed by: 200 operations",
		"waybar-extra.service: systemd rejected this service: Command /usr/bin/waybar-extra is not executable",
		"Restore can't apply yet: packages, services aren't ready.",
		"Fix packages: run `omarchy update`, then plan the restore again.",
		"omarchy-blueprint restore --defer packages,services",
		"omarchy-blueprint policy set restore <category> skip <target>",
		"+ install theme:tokyo-night",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "pkg-150") || strings.Contains(view, "--force") {
		t.Fatalf("blocked plan enumerates targets or offers Force:\n%s", view)
	}
}

func TestRenderPlanOffersDeferralOnlyForAFullRestore(t *testing.T) {
	view := renderPlanWithOptions(freshMachineRestorePlan(3), true, restorePlanOptions{SingleCategory: "packages"})
	if strings.Contains(view, "--defer") || !strings.Contains(view, "omarchy update") {
		t.Fatalf("single-category plan suggests deferral or loses the fix:\n%s", view)
	}
	ready := model.RestorePlan{Deferred: []string{"packages", "services"}, Operations: []model.Operation{{Provider: "themes", Resource: "theme:tokyo-night", Action: "install"}}}
	view = renderPlanWithOptions(ready, false, restorePlanOptions{})
	if !strings.Contains(view, "Deferred for later: packages, services (not part of this restore)") || strings.Contains(view, "can't apply") {
		t.Fatalf("deferred plan does not say what it leaves out:\n%s", view)
	}
}

func TestRestoreDeferLeavesBlockedCategoryOutOfTheRun(t *testing.T) {
	profileDir, deps := configSandbox(t)
	pluginDir := t.TempDir()
	deps.PluginDir = func() (string, error) { return pluginDir, nil }
	runner := deps.Runner.(*machineRunner)
	runner.plugins = map[string]bool{"omarchy.clock": true}
	for _, category := range []string{"plugins", "themes"} {
		if code, output := configRun(t, deps, profileDir, "capture", category); code != 0 {
			t.Fatalf("capture %s: %s", category, output)
		}
	}
	delete(runner.plugins, "omarchy.clock")
	if code, output := configRun(t, deps, profileDir, "restore", "--dry-run"); code != 0 || !strings.Contains(output, "restore --defer plugins") {
		t.Fatalf("blocked dry-run does not offer deferral: code=%d output=%s", code, output)
	}
	code, output := configRun(t, deps, profileDir, "--json", "restore", "--defer", "plugins", "--dry-run")
	if code != 0 {
		t.Fatalf("deferred dry-run failed: %s", output)
	}
	var envelope struct {
		Data struct {
			Plan model.RestorePlan `json:"plan"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Plan.Deferred) != 1 || envelope.Data.Plan.Deferred[0] != "plugins" {
		t.Fatalf("plan does not record the deferral: %+v", envelope.Data.Plan.Deferred)
	}
	for _, category := range envelope.Data.Plan.Compatibility.Categories {
		if category.Category == "plugins" {
			t.Fatalf("deferred category was still assessed: %+v", category)
		}
	}
	if code, output := configRun(t, deps, profileDir, "restore", "plugins", "--defer", "themes"); code == 0 || !strings.Contains(output, "cannot be combined") {
		t.Fatalf("--defer with a category argument was accepted: code=%d output=%s", code, output)
	}
}
