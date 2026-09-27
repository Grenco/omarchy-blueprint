package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func realCompatibilitySession(t *testing.T, deps Dependencies, profileDir string, provider stateProvider) *workflow.Session {
	t.Helper()
	session, err := workflow.Open(workflowDependencies(deps), workflow.Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetProviders([]workflow.Provider{restoreProviderAdapter{provider}}); err != nil {
		t.Fatal(err)
	}
	return session
}

func assertBlockedCompatibility(t *testing.T, plan model.RestorePlan, code string) {
	t.Helper()
	if len(plan.Compatibility.Categories) != 1 || plan.Compatibility.Categories[0].Authority != model.CompatibilityBlocked || len(plan.Operations) != 0 {
		t.Fatalf("unsafe intent not blocked before mutation: %+v", plan)
	}
	for _, finding := range plan.Compatibility.Categories[0].Findings {
		if finding.Code == code {
			var blocked *workflow.BlockedCompatibilityError
			if err := workflow.CheckRestoreApplicable(plan, true); !errors.As(err, &blocked) {
				t.Fatalf("plan not blocked by compatibility: %v", err)
			}
			return
		}
	}
	t.Fatalf("finding %s missing from %+v", code, plan.Compatibility)
}

func TestRealShellAdapterBlocksUnsupportedCurrentSchema(t *testing.T) {
	profileDir, deps := configSandbox(t)
	baseline, user := shellPathsFixture(t)
	setShellPaths(&deps, baseline, user)
	custom := strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 600`, 1)
	if err := os.WriteFile(user, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, output := configRun(t, deps, profileDir, "capture", "shell"); code != 0 {
		t.Fatalf("capture shell: %s", output)
	}
	if err := os.WriteFile(baseline, []byte(strings.Replace(defaultShellJSON, `"version": 1`, `"version": 2`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	session := realCompatibilitySession(t, deps, profileDir, shellStateProvider{deps: deps, opt: &options{profileDir: profileDir}})
	plan, err := session.PlanRestore(context.Background(), "shell", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBlockedCompatibility(t, plan, "shell.schema.unsupported")
	saved, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	saved.Policy.Restore = []policy.Rule{{Category: "shell", Target: "state", Setting: policy.SettingDisabled}}
	if err := profile.Save(profileDir, saved); err != nil {
		t.Fatal(err)
	}
	skipped, err := realCompatibilitySession(t, deps, profileDir, shellStateProvider{deps: deps, opt: &options{profileDir: profileDir}}).PlanRestore(context.Background(), "shell", nil)
	if err != nil || skipped.Compatibility.Categories[0].Applies || len(skipped.Operations) != 0 {
		t.Fatalf("explicit Shell policy Skip gained compatibility/mutation scope: %+v err=%v", skipped, err)
	}
}

func TestRealConfigAdapterKeepsUnsafeApplyInCompatibilityScope(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		exact      bool
		unsafe     func(*testing.T, string, string)
	}{
		{"unsupported", "config.surface.unsupported", false, func(t *testing.T, baseline, user string) {
			if err := os.Remove(user); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(user, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"ambiguous-exact", "config.baseline.ambiguous", true, func(t *testing.T, baseline, user string) {
			if err := os.WriteFile(baseline, []byte("new default"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(user, []byte("target drift"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profileDir, deps := configSandbox(t)
			baseRoot, userRoot, err := deps.ConfigDirs()
			if err != nil {
				t.Fatal(err)
			}
			deps.HomeDir = func() (string, error) { return filepath.Dir(userRoot), nil }
			deps.PluginDir = func() (string, error) { return t.TempDir(), nil }
			baseline, user := filepath.Join(baseRoot, "hypr", "bindings.lua"), filepath.Join(userRoot, "hypr", "bindings.lua")
			if err := os.WriteFile(user, []byte("captured custom"), 0o644); err != nil {
				t.Fatal(err)
			}
			captured, err := profile.Load(profileDir)
			if err != nil {
				t.Fatal(err)
			}
			path := ".config/hypr/bindings.lua"
			captured.Config.Files = []profile.ConfigFile{{Path: path, Hash: appHash(t, "captured custom"), BaselineHash: appHash(t, "default")}}
			captured.Manifest.Capture.Config = true
			for name, content := range map[string]string{"files": "captured custom", "baseline": "default"} {
				snapshot := filepath.Join(profileDir, "config", name, path)
				if err := os.MkdirAll(filepath.Dir(snapshot), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(snapshot, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := profile.Save(profileDir, captured); err != nil {
				t.Fatal(err)
			}
			tc.unsafe(t, baseline, user)
			session := realCompatibilitySession(t, deps, profileDir, configStateProvider{deps: deps, opt: &options{profileDir: profileDir}})
			var opts *policy.RestoreOptions
			if tc.exact {
				opts = &policy.RestoreOptions{Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceExact}
			}
			plan, err := session.PlanRestore(context.Background(), "config", opts)
			if err != nil {
				t.Fatal(err)
			}
			if tc.exact {
				if plan.Compatibility.Categories[0].State != model.CompatibilityUnknown || plan.Compatibility.Categories[0].Authority != model.CompatibilityReduced || len(plan.Operations) != 0 {
					t.Fatalf("ambiguous Exact intent gained authority: %+v", plan)
				}
				result, err := session.ApplyRestore(context.Background(), "config", opts)
				if err != nil || result.Verification.OK || len(result.Verification.Missing) == 0 {
					t.Fatalf("withheld Exact intent falsely verified: result=%+v err=%v", result, err)
				}
			} else {
				assertBlockedCompatibility(t, plan, tc.want)
			}
			if len(plan.Compatibility.Categories[0].Findings) != 1 || plan.Compatibility.Categories[0].Findings[0].Code != tc.want {
				t.Fatalf("lost %s: %+v", tc.want, plan.Compatibility)
			}
			saved, err := profile.Load(profileDir)
			if err != nil {
				t.Fatal(err)
			}
			saved.Policy.Restore = []policy.Rule{{Category: "config", Target: path, Setting: policy.SettingDisabled}}
			if err := profile.Save(profileDir, saved); err != nil {
				t.Fatal(err)
			}
			skipped, err := realCompatibilitySession(t, deps, profileDir, configStateProvider{deps: deps, opt: &options{profileDir: profileDir}}).PlanRestore(context.Background(), "config", opts)
			if err != nil || skipped.Compatibility.Categories[0].Applies || len(skipped.Operations) != 0 {
				t.Fatalf("explicit Config policy Skip gained compatibility/mutation scope: %+v err=%v", skipped, err)
			}
			saved.Policy.Restore = nil
			saved.Config.Excluded = []string{path}
			if err := profile.Save(profileDir, saved); err != nil {
				t.Fatal(err)
			}
			excluded, err := realCompatibilitySession(t, deps, profileDir, configStateProvider{deps: deps, opt: &options{profileDir: profileDir}}).PlanRestore(context.Background(), "config", opts)
			if err != nil || excluded.Compatibility.Categories[0].Applies || len(excluded.Operations) != 0 {
				t.Fatalf("Config Excluded gained compatibility/mutation scope: %+v err=%v", excluded, err)
			}
		})
	}
}

func TestRealPluginsAdapterBlocksMissingSavedBuiltin(t *testing.T) {
	profileDir, deps := configSandbox(t)
	pluginDir := t.TempDir()
	deps.PluginDir = func() (string, error) { return pluginDir, nil }
	runner := deps.Runner.(*machineRunner)
	runner.plugins = map[string]bool{"omarchy.clock": true}
	if code, output := configRun(t, deps, profileDir, "capture", "plugins"); code != 0 {
		t.Fatalf("capture plugins: %s", output)
	}
	delete(runner.plugins, "omarchy.clock")
	session := realCompatibilitySession(t, deps, profileDir, &pluginsStateProvider{deps: deps, opt: &options{profileDir: profileDir}})
	plan, err := session.PlanRestore(context.Background(), "plugins", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBlockedCompatibility(t, plan, "plugins.first_party.unavailable")
	saved, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	saved.Policy.Restore = []policy.Rule{{Category: "plugins", Target: "plugin:omarchy.clock", Setting: policy.SettingDisabled}}
	if err := profile.Save(profileDir, saved); err != nil {
		t.Fatal(err)
	}
	skipped, err := realCompatibilitySession(t, deps, profileDir, &pluginsStateProvider{deps: deps, opt: &options{profileDir: profileDir}}).PlanRestore(context.Background(), "plugins", nil)
	if err != nil || skipped.Compatibility.Categories[0].Applies || len(skipped.Operations) != 0 {
		t.Fatalf("explicit builtin policy Skip gained compatibility/mutation scope: %+v err=%v", skipped, err)
	}
}

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
