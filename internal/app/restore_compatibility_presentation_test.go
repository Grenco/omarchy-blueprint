package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func TestRestoreCompatibilityJSONRetainsContextAndExplicitUnknown(t *testing.T) {
	f := newElevatedRestoreFixture(t)
	f.runner.repos, f.runner.dbPath = []string{"core"}, t.TempDir()
	code, output := f.run("--profile", f.profileDir, "--json", "restore", "packages", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run code=%d output=%s", code, output)
	}
	var envelope struct {
		Data struct {
			Plan map[string]json.RawMessage `json:"plan"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"omarchy_from", "omarchy_to", "compatibility"} {
		if _, exists := envelope.Data.Plan[key]; !exists {
			t.Fatalf("missing plan.%s: %s", key, output)
		}
	}
	var report map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data.Plan["compatibility"], &report); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"profile_last_capture", "target", "categories"} {
		if _, exists := report[key]; !exists {
			t.Fatalf("missing compatibility.%s: %s", key, output)
		}
	}
	var categories []model.CompatibilityCategory
	if err := json.Unmarshal(report["categories"], &categories); err != nil || len(categories) != 1 || categories[0].Category != "packages" || categories[0].State != model.CompatibilityUnknown || categories[0].Authority != model.CompatibilityBlocked {
		t.Fatalf("unknown package compatibility lost: %+v err=%v", categories, err)
	}
	code2, output2 := f.run("--profile", f.profileDir, "--json", "restore", "packages", "--dry-run")
	if code2 != 0 || output2 != output {
		t.Fatalf("dry-run compatibility JSON is not deterministic: code=%d\nfirst=%s\nsecond=%s", code2, output, output2)
	}
}

func TestRenderCompatibilityBeforeRequirementsAndOperations(t *testing.T) {
	plan := model.RestorePlan{
		Compatibility: model.CompatibilityReport{
			ProfileLastCapture: model.CompatibilityEnvironment{},
			Target:             model.CompatibilityEnvironment{Known: true, OmarchyVersion: "4.2.0", OmarchyChannel: "stable"},
			Categories: []model.CompatibilityCategory{
				{Category: "resources", Applies: true, State: model.CompatibilitySupported, Authority: model.CompatibilityUnchanged, Evidence: []model.CompatibilityEvidence{{Kind: "portable", Summary: "portable copy"}}},
				{Category: "hooks", Applies: true, State: model.CompatibilityUnknown, Authority: model.CompatibilityReduced, Findings: []model.CompatibilityFinding{{Code: "hooks.lifecycle.unknown", Target: "startup", State: model.CompatibilityUnknown, Authority: model.CompatibilityReduced, Summary: "lifecycle not established"}}},
				{Category: "plugins", Applies: true, State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Findings: []model.CompatibilityFinding{{Code: "plugins.first_party.unavailable", Target: "plugin:clock", State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Summary: "first-party plugin unavailable"}}},
			},
		},
		Requirements: []model.Requirement{{ID: "packages.metadata", Reason: "package metadata unavailable", Remediation: []string{"omarchy", "update"}}},
		Operations:   []model.Operation{{Provider: "packages", Resource: "official:alacritty", Action: "install"}},
	}
	view := renderPlanWithOptions(plan, false, restorePlanOptions{})
	for _, want := range []string{"Profile last capture: unknown", "Target Omarchy: 4.2.0 (stable)", "Resources: Supported · Unchanged", "Hooks: Unknown · Reduced", "lifecycle not established", "Plugins: Incompatible · Blocked", "first-party plugin unavailable"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
	compatibility, requirements, operation := strings.Index(view, "Compatibility"), strings.Index(view, "Requires before applying"), strings.Index(view, "+ install official:alacritty")
	if compatibility < 0 || requirements <= compatibility || operation <= requirements || strings.Contains(view, "Source Omarchy") {
		t.Fatalf("compatibility/readiness/work ordering or provenance wording wrong:\n%s", view)
	}
}

func TestRestoreCompatibilityForceCannotPromptOrApplyBlockedPlugin(t *testing.T) {
	profileDir, deps := configSandbox(t)
	pluginDir := t.TempDir()
	deps.PluginDir = func() (string, error) { return pluginDir, nil }
	runner := deps.Runner.(*machineRunner)
	runner.plugins = map[string]bool{"omarchy.clock": true}
	if code, output := configRun(t, deps, profileDir, "capture", "plugins"); code != 0 {
		t.Fatalf("capture plugins: %s", output)
	}
	delete(runner.plugins, "omarchy.clock")
	code, output := configRun(t, deps, profileDir, "restore", "plugins", "--force", "--yes")
	if code == 0 || !strings.Contains(output, "Compatibility") || !strings.Contains(output, "Incompatible · Blocked") || !strings.Contains(output, "restore compatibility blocks") || strings.Contains(output, "Apply this restore?") {
		t.Fatalf("Force bypassed compatibility or hid plan: code=%d output=%s", code, output)
	}
	for _, command := range runner.allCommands {
		joined := strings.Join(command, " ")
		if strings.Contains(joined, "omarchy plugin add") || strings.Contains(joined, "omarchy plugin remove") {
			t.Fatalf("blocked Restore executed mutation: %q", joined)
		}
	}
}

func TestRestoreCompatibilityHumanYesShowsPlanOnceBeforeApply(t *testing.T) {
	f := newElevatedRestoreFixture(t)
	code, output := f.run("--profile", f.profileDir, "restore", "packages", "--yes")
	if code != 0 || !f.runner.official["alacritty"] || len(f.runner.interactiveCommands) != 1 {
		t.Fatalf("approved package restore failed: code=%d output=%s commands=%v", code, output, f.runner.interactiveCommands)
	}
	if strings.Count(output, "Compatibility:\n") != 1 || !strings.Contains(output, "Packages:") || !strings.Contains(output, "+ install official:alacritty") || strings.Contains(output, "Apply this restore?") {
		t.Fatalf("successful --yes omitted or duplicated plan inspection: %s", output)
	}
	if plan, result := strings.Index(output, "Compatibility:\n"), strings.Index(output, "Restore verified"); result < 0 || plan < 0 || plan >= result {
		t.Fatalf("compatibility did not precede execution result: %s", output)
	}
}
