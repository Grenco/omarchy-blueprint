package app

import (
	"encoding/json"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncApplyHasExplicitOneShotFlagsAndNoPersistedResolution(t *testing.T) {
	_, deps := configSandbox(t)
	cmd, _, err := newRoot(deps).Find([]string{"sync", "apply"})
	if err != nil || cmd.Name() != "apply" {
		t.Fatalf("missing apply: %v", err)
	}
	for _, flag := range []string{"keep-local", "accept-remote", "keep-machine", "keep-profile", "defer", "yes", "dry-run", "no-fetch", "exact", "force"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("missing flag %s", flag)
		}
	}
	for _, choice := range []string{"reconcile-upstream", "adopt-machine", "cancel"} {
		cmd, _, err := newRoot(deps).Find([]string{"sync", "bootstrap", choice})
		if err != nil || cmd.Name() != choice {
			t.Fatalf("missing bootstrap choice %s", choice)
		}
	}
}

func TestSyncApplyDryRunJSONAndBlockedExecutionDoNotWrite(t *testing.T) {
	root, deps := configSandbox(t)
	deps.IsTTY = func() bool { return false }
	before, err := os.ReadFile(filepath.Join(root, "profile.toml"))
	if err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, root, "--json", "sync", "apply", "--no-fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run failed: %s", out)
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Sync syncengine.Plan `json:"sync"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || !envelope.OK || len(envelope.Data.Sync.Blockers) == 0 {
		t.Fatalf("missing typed blocked inspection: %s (%v)", out, err)
	}
	if code, _ := configRun(t, deps, root, "sync", "apply", "--no-fetch", "--yes"); code == 0 {
		t.Fatal("blocked execution returned success")
	}
	after, err := os.ReadFile(filepath.Join(root, "profile.toml"))
	if err != nil || string(before) != string(after) {
		t.Fatal("preview/blocked execution wrote profile")
	}
	state, _ := deps.StateHome()
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatal("preview/blocked execution wrote local Sync state")
	}
	if strings.Contains(out, root) || strings.Contains(out, state) {
		t.Fatal("JSON leaked private paths")
	}
}

func TestSyncBootstrapCancelDoesNotOpenProfile(t *testing.T) {
	root, deps := configSandbox(t)
	code, out := configRun(t, deps, filepath.Join(root, "does-not-exist"), "--json", "sync", "bootstrap", "cancel")
	if code != 0 || !strings.Contains(out, `"cancelled": true`) {
		t.Fatalf("cancel: %d %s", code, out)
	}
	state, _ := deps.StateHome()
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatal("cancel created local state")
	}
}

func TestSyncResolutionParsingPreservesProviderIdentityAndRejectsAmbiguity(t *testing.T) {
	choices, err := parseSyncChoices(map[string][]string{"keep-local": {"packages:official:git"}, "keep-machine": {"packages:official:git"}})
	if err != nil || len(choices) != 1 || choices[0].Key.Target != "official:git" || choices[0].Resolution != syncengine.ResolutionKeepLocal || choices[0].MachineResolution != syncengine.MachineResolutionKeepMachine {
		t.Fatalf("choices=%#v error=%v", choices, err)
	}
	for _, flags := range []map[string][]string{
		{"keep-local": {"packages:official:git", "packages:official:git"}},
		{"keep-local": {"packages:official:git"}, "accept-remote": {"packages:official:git"}},
		{"keep-machine": {"packages:official:git"}, "keep-profile": {"packages:official:git"}},
		{"keep-local": {"missing-separator"}},
	} {
		if _, err := parseSyncChoices(flags); err == nil {
			t.Fatal("accepted ambiguous choices")
		}
	}
}

func TestSyncParsedChoicesMustNameAnActualMeaningfulConflict(t *testing.T) {
	state := syncengine.State{Kind: syncengine.StatePresent, Fingerprint: "canonical"}
	key := syncengine.TargetKey{Category: "packages", Target: "official:git"}
	plan, err := syncengine.Reconcile(syncengine.PlanRequest{
		Identity: syncengine.Identity{Base: strings.Repeat("a", 40), Local: strings.Repeat("a", 40), Remote: strings.Repeat("a", 40), Machine: "desktop", Branch: "main", Upstream: "origin/main", RemoteFingerprint: strings.Repeat("b", 64), ControlFingerprint: strings.Repeat("c", 64)},
		Targets:  []syncengine.TargetInput{{Key: key, Base: state, Local: state, Remote: state, Machine: state}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, flags := range []map[string][]string{
		{"keep-local": {"packages:official:missing"}},
		{"keep-local": {"packages:official:git"}},
		{"keep-machine": {"packages:official:git"}},
	} {
		choices, err := parseSyncChoices(flags)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := workflow.ResolveSyncPlan(plan, choices); err == nil {
			t.Fatal("unknown or meaningless choice accepted")
		}
	}
}

func TestSyncApplyWithoutApprovalRejectsBeforeObservation(t *testing.T) {
	_, deps := configSandbox(t)
	deps.IsTTY = func() bool { return false }
	cmd := newRoot(deps)
	cmd.SetArgs([]string{"--json", "sync", "apply", "--no-fetch"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("noninteractive execution did not require approval: %v", err)
	}
}
