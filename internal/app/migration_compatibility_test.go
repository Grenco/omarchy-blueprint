package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestConfigConvergenceFiltersTombstonesOnlyUnderAdditive(t *testing.T) {
	saved := profile.Configs{
		Files:   []profile.ConfigFile{{Path: ".config/app/settings.toml", Hash: "present"}},
		Deletes: []profile.ConfigDelete{{Path: ".config/app/obsolete.toml", BaselineHash: "baseline"}},
	}
	additive := filterConfigForConvergence(saved, policy.ConvergenceAdditive)
	if len(additive.Files) != 1 || len(additive.Deletes) != 0 || len(saved.Deletes) != 1 {
		t.Fatalf("Additive changed present intent or kept tombstone: saved=%+v filtered=%+v", saved, additive)
	}
	exact := filterConfigForConvergence(saved, policy.ConvergenceExact)
	if len(exact.Files) != 1 || len(exact.Deletes) != 1 {
		t.Fatalf("Exact dropped desired absence: %+v", exact)
	}
}

func TestRestoreCompatibilityAppearsInPublicDryRunJSON(t *testing.T) {
	profileDir, deps := configSandbox(t)
	code, output := configRun(t, deps, profileDir, "--json", "restore", "packages", "--dry-run")
	if code != 0 {
		t.Fatalf("JSON dry-run failed: %s", output)
	}
	var envelope struct {
		Data struct {
			Plan map[string]json.RawMessage `json:"plan"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"profile_schema", "omarchy_from", "omarchy_to", "compatibility"} {
		if _, ok := envelope.Data.Plan[field]; !ok {
			t.Fatalf("Restore JSON lost %s: %s", field, output)
		}
	}
	var report struct {
		ProfileLastCapture model.CompatibilityEnvironment `json:"profile_last_capture"`
		Target             model.CompatibilityEnvironment `json:"target"`
		Categories         []model.CompatibilityCategory  `json:"categories"`
	}
	if err := json.Unmarshal(envelope.Data.Plan["compatibility"], &report); err != nil || !report.Target.Known || len(report.Categories) != 1 || report.Categories[0].Category != "packages" {
		t.Fatalf("Restore JSON compatibility = %+v err=%v", report, err)
	}
}

func TestConfigPlanAndVerifyUseTheSameConvergenceIntent(t *testing.T) {
	profileDir, deps := configSandbox(t)
	baselineRoot, userRoot, err := deps.ConfigDirs()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	deps.HomeDir = func() (string, error) { return home, nil }
	deps.PluginDir = func() (string, error) { return "", errors.New("not configured") }
	path := "hypr/bindings.lua"
	baseline, err := os.ReadFile(filepath.Join(baselineRoot, path))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(profileDir, "config", "baseline", path)
	if err := os.MkdirAll(filepath.Dir(snapshot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot, baseline, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userRoot, path), baseline, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	d.Config.Deletes = []profile.ConfigDelete{{Path: path, BaselineHash: appHash(t, string(baseline))}}
	p := configStateProvider{deps: deps, opt: &options{profileDir: profileDir}}
	for _, tc := range []struct {
		mode       policy.ConvergenceMode
		wantDelete bool
	}{{policy.ConvergenceAdditive, false}, {policy.ConvergenceExact, true}} {
		rc := workflow.RestoreContext{Options: policy.RestoreOptions{Conflicts: policy.ConflictSafe, Convergence: tc.mode}, Targets: map[string]workflow.RestoreDecision{path: {Restore: true, Resolved: true}}}
		plan, err := p.Plan(context.Background(), d, omarchy.Info{Version: "4.0.4"}, rc)
		if err != nil {
			t.Fatal(err)
		}
		deleted := false
		for _, op := range plan.Operations {
			deleted = deleted || op.Delete != nil
		}
		if deleted != tc.wantDelete || plan.Compatibility.Applies != tc.wantDelete {
			t.Fatalf("mode=%s plan=%+v", tc.mode, plan)
		}
		verified, err := p.Verify(context.Background(), d, rc)
		if err != nil || verified.OK == tc.wantDelete {
			t.Fatalf("mode=%s verify=%+v err=%v", tc.mode, verified, err)
		}
	}
}
