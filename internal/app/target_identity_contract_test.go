package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// TestProviderTargetIdentityContract runs the real provider adapters, as
// workflow sees them, against a sandbox that genuinely differs from its
// profile. Every Diff change and every planned Restore operation must map to
// a target key the same provider's InspectTargets reports, or decline
// explicitly; otherwise Capture review and Overview would silently
// misattribute real differences.
func TestProviderTargetIdentityContract(t *testing.T) {
	profileDir, deps := configSandbox(t)
	hooksDir := t.TempDir()
	deps.HooksDir = func() (string, error) { return hooksDir, nil }
	_, userRoot, err := deps.ConfigDirs()
	if err != nil {
		t.Fatal(err)
	}
	// Keep every provider inside the sandbox rather than Execute's real
	// defaults for the user's home, plugins, and Mise configuration.
	deps.HomeDir = func() (string, error) { return filepath.Dir(userRoot), nil }
	pluginDir := t.TempDir()
	deps.PluginDir = func() (string, error) { return pluginDir, nil }
	miseConfig := filepath.Join(t.TempDir(), "mise.toml")
	deps.MiseGlobalConfig = func() (string, error) { return miseConfig, nil }
	deps.Hostname = func() (string, error) { return "sandbox", nil }
	deps.ResourceLinkRoots = func(string) []resourcesprovider.LinkSearchRoot { return nil }
	write := func(path, content string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(hooksDir, "post-update.d", "kept"), "#!/bin/bash\necho kept\n", 0o755)
	write(filepath.Join(hooksDir, "post-update.d", "removed"), "#!/bin/bash\necho removed\n", 0o755)
	write(filepath.Join(userRoot, "hypr", "kept.lua"), "kept", 0o644)
	write(filepath.Join(userRoot, "hypr", "removed.lua"), "removed", 0o644)
	runner := deps.Runner.(*machineRunner)
	// Two saved packages so Restore batches their reinstall into one
	// operation ("official:bat,ripgrep").
	runner.official["ripgrep"], runner.official["bat"] = true, true
	for _, category := range []string{"packages", "config", "hooks"} {
		if code, out := configRun(t, deps, profileDir, "capture", category); code != 0 {
			t.Fatalf("capture %s code=%d out=%s", category, code, out)
		}
	}

	// Diverge the machine from the profile in every way these categories model.
	write(filepath.Join(hooksDir, "post-update.d", "kept"), "#!/bin/bash\necho changed\n", 0o755)
	write(filepath.Join(hooksDir, "post-update.d", "new"), "#!/bin/bash\necho new\n", 0o755)
	if err := os.Remove(filepath.Join(hooksDir, "post-update.d", "removed")); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(userRoot, "hypr", "kept.lua"), "changed", 0o644)
	write(filepath.Join(userRoot, "hypr", "new.lua"), "new", 0o644)
	if err := os.Remove(filepath.Join(userRoot, "hypr", "removed.lua")); err != nil {
		t.Fatal(err)
	}
	delete(runner.official, "ripgrep")
	delete(runner.official, "bat")
	runner.official["fzf"] = true

	opt := &options{profileDir: profileDir}
	session, err := workflow.Open(workflowDependencies(deps), workflow.Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	providers := workflowProviders(deps, opt)
	if err := session.SetProviders(providers); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	inventories := map[string]map[string]bool{}
	for _, provider := range providers {
		targets, err := provider.InspectTargets(ctx, session.Profile())
		if err != nil {
			t.Fatalf("%s InspectTargets: %v", provider.ID(), err)
		}
		inventory := map[string]bool{}
		for _, target := range targets {
			inventory[target.Key] = true
		}
		inventories[provider.ID()] = inventory
	}

	attributed := map[string]int{}
	for _, provider := range providers {
		changes, err := provider.Diff(ctx, session.Profile())
		if err != nil {
			t.Fatalf("%s Diff: %v", provider.ID(), err)
		}
		resolver, ok := provider.(workflow.ChangeTargetResolver)
		if !ok {
			t.Fatalf("%s does not attribute Diff changes", provider.ID())
		}
		for _, change := range changes {
			key, ok := resolver.ChangeTargetKey(change)
			if !ok {
				t.Errorf("%s cannot attribute Diff change %+v", provider.ID(), change)
				continue
			}
			if key != "" && !inventories[provider.ID()][key] {
				t.Errorf("%s attributes %+v to %q, which InspectTargets does not report", provider.ID(), change, key)
				continue
			}
			attributed[provider.ID()]++
		}
	}

	plan, err := session.PlanRestore(ctx, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]workflow.Provider{}
	for _, provider := range providers {
		byID[provider.ID()] = provider
	}
	planned := map[string]int{}
	for _, op := range plan.Operations {
		resolver, ok := byID[op.Provider].(workflow.RestoreTargetResolver)
		if !ok {
			t.Errorf("%s does not attribute Restore operations", op.Provider)
			continue
		}
		keys, ok := resolver.RestoreOperationTargetKeys(op)
		if !ok {
			t.Errorf("%s cannot attribute Restore operation %s (%s)", op.Provider, op.ID, op.Resource)
			continue
		}
		// A supporting operation such as a reload targets nothing.
		for _, key := range keys {
			if !inventories[op.Provider][key] {
				t.Errorf("%s Restore operation %s (%s) maps to %q, which InspectTargets does not report", op.Provider, op.ID, op.Resource, key)
				continue
			}
			planned[op.Provider]++
		}
	}

	// Approving an unchanged review of these real categories must capture:
	// the in-transaction re-checks must not report false changes.
	ids := []string{"packages", "config", "hooks"}
	review, err := session.InspectCaptureMany(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.CaptureApproved(ctx, ids, review); err != nil {
		t.Fatalf("approved Capture of an unchanged review: %v", err)
	}
	if after, err := session.InspectCaptureMany(ctx, ids); err != nil {
		t.Fatal(err)
	} else if changes := after.ChangesFrom(review); len(changes) == 0 {
		t.Fatal("approved Capture changed nothing")
	}

	if planned["packages"] < 2 {
		t.Errorf("fixture planned %d package targets, want the batched reinstall of both", planned["packages"])
	}
	for _, category := range []string{"packages", "config", "hooks"} {
		if attributed[category] == 0 {
			t.Errorf("fixture produced no attributable %s Diff changes; the contract was not exercised", category)
		}
		if planned[category] == 0 {
			t.Errorf("fixture produced no %s Restore operations; the contract was not exercised", category)
		}
	}
}
