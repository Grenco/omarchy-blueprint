package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type runnerFunc func(context.Context, string, ...string) (string, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f(ctx, name, args...)
}

func TestCaptureGitAndLocalPlugins(t *testing.T) {
	user, profileDir := t.TempDir(), t.TempDir()
	previous := filepath.Join(profileDir, "plugins", "local", "mine.local", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(previous), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previous, []byte(`{"id":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"acme.git", "mine.local"} {
		if err := os.MkdirAll(filepath.Join(user, id), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(user, "acme.git", ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "mine.local", "manifest.json"), []byte(`{"id":"mine.local"}`), 0644); err != nil {
		t.Fatal(err)
	}
	catalog := `[{"id":"acme.git","enabled":true,"firstParty":false},{"id":"mine.local","enabled":false,"firstParty":false,"clonedFrom":"omarchy.clock"}]`
	runner := runnerFunc(func(_ context.Context, name string, args ...string) (string, error) {
		joined := name + " " + strings.Join(args, " ")
		switch {
		case joined == "omarchy plugin list --json":
			return catalog, nil
		case strings.HasSuffix(joined, "remote get-url origin"):
			return "https://token@example.test/acme.git\n", nil
		case strings.HasSuffix(joined, "rev-parse HEAD"):
			return strings.Repeat("a", 40) + "\n", nil
		case strings.HasSuffix(joined, "status --porcelain --untracked-files=all"):
			return "", nil
		default:
			return "", fmt.Errorf("unexpected %s", joined)
		}
	})
	provider := Provider{Runner: runner, UserDir: user, ProfileDir: profileDir}
	got, err := provider.Capture(context.Background(), profile.Plugins{}, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if got.Items[0].Source != "git" || got.Items[0].URL != "https://example.test/acme.git" {
		t.Fatalf("git=%#v", got.Items[0])
	}
	if got.Items[1].Source != "local" || got.Items[1].Hash == "" {
		t.Fatalf("local=%#v", got.Items[1])
	}
	if _, err := os.Stat(filepath.Join(profileDir, "plugins", "local", "mine.local", "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if err := provider.RollbackCapture(); err != nil {
		t.Fatal(err)
	}
	if restored, err := os.ReadFile(previous); err != nil || string(restored) != `{"id":"old"}` {
		t.Fatalf("restored snapshot=%q err=%v", restored, err)
	}
}

func TestCapturePreservesExistingArtifactWhenDisabled(t *testing.T) {
	user, profileDir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(user, "mine.local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "mine.local", "manifest.json"), []byte(`{"id":"new"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(profileDir, "plugins", "local", "mine.local", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte(`{"id":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := runnerFunc(func(_ context.Context, name string, args ...string) (string, error) {
		if name+" "+strings.Join(args, " ") == "omarchy plugin list --json" {
			return `[{"id":"mine.local","enabled":true,"firstParty":false}]`, nil
		}
		return "", fmt.Errorf("unexpected command")
	})
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "mine.local", Source: "local", Hash: "old-hash"}}}
	provider := Provider{Runner: runner, UserDir: user, ProfileDir: profileDir}

	got, err := provider.Capture(context.Background(), saved, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Hash != "old-hash" {
		t.Fatalf("items = %#v, want the preserved saved metadata (old hash), not a fresh re-hash of the changed local content", got.Items)
	}
	content, err := os.ReadFile(filepath.Join(profileDir, "plugins", "local", "mine.local", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != `{"id":"old"}` {
		t.Fatalf("staged artifact = %q, want the preserved prior content, not the changed local file", content)
	}
}

func TestCaptureTombstonesPluginRemovedLocally(t *testing.T) {
	user, profileDir := t.TempDir(), t.TempDir()
	runner := runnerFunc(func(_ context.Context, name string, args ...string) (string, error) {
		if name+" "+strings.Join(args, " ") == "omarchy plugin list --json" {
			return `[]`, nil
		}
		return "", fmt.Errorf("unexpected command")
	})
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "gone.local", Source: "local", Hash: "abc"}}}
	provider := Provider{Runner: runner, UserDir: user, ProfileDir: profileDir}

	got, err := provider.Capture(context.Background(), saved, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Absent) != 1 || got.Absent[0].ID != "gone.local" || got.Absent[0].Hash != "abc" {
		t.Fatalf("absent = %#v, want a tombstone for gone.local preserving its prior hash", got.Absent)
	}
	for _, item := range got.Items {
		if item.ID == "gone.local" {
			t.Fatalf("items = %#v, want gone.local removed from present items", got.Items)
		}
	}
}

// TestCaptureTwiceWhileStillAbsentPreservesTombstoneProvenance is a
// regression for a review finding on PR 3: re-capturing while a plugin stays
// desired-absent must carry the EXISTING tombstone's provenance forward
// (Source/URL/Revision/Hash), not rebuild an ID-only Plugin -- later Exact
// removal needs that provenance to prove ownership before deleting anything.
// TestPrepareStopManagingArtifactStagesRenameCommitOrRollback is a
// regression for a review finding on PR 3: Stop Managing must not delete a
// plugin's local clone artifact immediately, since a caller that then fails
// to save the profile would leave it referencing a destroyed artifact.
// PrepareStopManagingArtifact stages the removal by rename; RollbackCapture
// must restore the original directory and its content byte-for-byte, and
// FinalizeCapture (only called once the caller knows the save succeeded)
// must permanently delete it.
func TestPrepareStopManagingArtifactStagesRenameCommitOrRollback(t *testing.T) {
	profileDir := t.TempDir()
	artifact := filepath.Join(profileDir, "plugins", "local", "acme")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifact, "init.lua"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &Provider{ProfileDir: profileDir}
	if err := p.PrepareStopManagingArtifact("acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatal("artifact must be staged out of its original location, not left in place")
	}

	if err := p.RollbackCapture(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(artifact, "init.lua"))
	if err != nil || string(body) != "original" {
		t.Fatalf("rollback did not restore original content: body=%q err=%v", body, err)
	}

	if err := p.PrepareStopManagingArtifact("acme"); err != nil {
		t.Fatal(err)
	}
	if err := p.FinalizeCapture(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatal("finalize must permanently delete the staged artifact")
	}
	entries, err := os.ReadDir(filepath.Join(profileDir, "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".stop-managing-") {
			t.Fatalf("finalize left a staging backup behind: %s", entry.Name())
		}
	}
}

func TestCaptureTwiceWhileStillAbsentPreservesTombstoneProvenance(t *testing.T) {
	user, profileDir := t.TempDir(), t.TempDir()
	runner := runnerFunc(func(_ context.Context, name string, args ...string) (string, error) {
		if name+" "+strings.Join(args, " ") == "omarchy plugin list --json" {
			return `[]`, nil
		}
		return "", fmt.Errorf("unexpected command")
	})
	saved := profile.Plugins{Absent: []profile.Plugin{{ID: "gone.local", Source: "local", Hash: "abc"}}}
	provider := Provider{Runner: runner, UserDir: user, ProfileDir: profileDir}

	got, err := provider.Capture(context.Background(), saved, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Absent) != 1 || got.Absent[0].ID != "gone.local" || got.Absent[0].Source != "local" || got.Absent[0].Hash != "abc" {
		t.Fatalf("absent = %#v, want the existing tombstone's provenance preserved, not rebuilt ID-only", got.Absent)
	}
}

func TestCaptureNeverTombstonesAFirstPartyPlugin(t *testing.T) {
	user, profileDir := t.TempDir(), t.TempDir()
	// "omarchy.clock" was previously known present; the catalog no longer
	// reports it at all (e.g. removed upstream), so a naive merge would see
	// it as present -> absent and tombstone it were first-party plugins not
	// excluded from Capture merge entirely.
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}}}
	runner := runnerFunc(func(_ context.Context, name string, args ...string) (string, error) {
		if name+" "+strings.Join(args, " ") == "omarchy plugin list --json" {
			return `[]`, nil
		}
		return "", fmt.Errorf("unexpected command")
	})
	provider := Provider{Runner: runner, UserDir: user, ProfileDir: profileDir}

	got, err := provider.Capture(context.Background(), saved, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Absent) != 0 {
		t.Fatalf("absent = %#v, want no tombstones for a first-party plugin no longer reported", got.Absent)
	}
}

func TestLocalRestorePlanUsesDependentValidationChain(t *testing.T) {
	p := Provider{UserDir: t.TempDir(), ProfileDir: t.TempDir()}
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "mine.local", Source: "local", Hash: "abc", Enabled: true}}}
	plan := p.Plan(saved, profile.Plugins{}, 1, "4", "4", Semantics{ManageEnabled: true})
	if len(plan.Operations) != 4 {
		t.Fatalf("operations=%#v", plan.Operations)
	}
	if plan.Operations[0].Action != "validate" || plan.Operations[1].DependsOn[0] != plan.Operations[0].ID || plan.Operations[3].Risk != model.RiskHigh {
		t.Fatalf("operations=%#v", plan.Operations)
	}
}

func TestGitRestorePlanRequiresTrustAndRevalidatesPinnedRevision(t *testing.T) {
	p := Provider{UserDir: t.TempDir()}
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "acme.weather", Source: "git", URL: "https://example.test/acme.git", Revision: strings.Repeat("a", 40), Enabled: true}}}
	plan := p.Plan(saved, profile.Plugins{}, 1, "4", "4", Semantics{ManageEnabled: true})
	if len(plan.Operations) != 5 {
		t.Fatalf("operations=%#v", plan.Operations)
	}
	if plan.Operations[0].Risk != model.RiskHigh || !reflect.DeepEqual(plan.Operations[0].Command, []string{"omarchy", "plugin", "add", "https://example.test/acme.git", "--yes"}) {
		t.Fatalf("install=%#v", plan.Operations[0])
	}
	if plan.Operations[2].Action != "validate" || plan.Operations[2].DependsOn[0] != plan.Operations[1].ID || plan.Operations[4].DependsOn[0] != plan.Operations[3].ID {
		t.Fatalf("dependencies=%#v", plan.Operations)
	}
}

func TestDetectDiffPlanAndVerify(t *testing.T) {
	json := `[{"id":"omarchy.clock","enabled":true,"firstParty":true,"canDisable":true}]`
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return json, nil })}
	current, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current.Items, []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}}) {
		t.Fatalf("plugins=%#v", current.Items)
	}
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Enabled: false}}}
	if len(Diff(saved, current, Semantics{ManageEnabled: true})) != 1 {
		t.Fatal("expected drift")
	}
	plan := p.Plan(saved, current, 1, "4", "4", Semantics{ManageEnabled: true})
	if got := plan.Operations[0].Command; !reflect.DeepEqual(got, []string{"omarchy", "plugin", "disable", "omarchy.clock"}) {
		t.Fatalf("command=%#v", got)
	}
	if Verify(saved, current, Semantics{ManageEnabled: true}).OK {
		t.Fatal("verification unexpectedly passed")
	}
}

// TestPlanAndVerifyIgnoreDesiredAbsenceTombstones is Task 23's PR 3 safety
// gate: a Capture-produced desired-absence tombstone is write-only today --
// Capture writes it, but Restore's Plan/Verify never read Absent -- so it
// cannot cause an unexpected removal, skip, or verification failure until
// PR 4 activates Restore-side policy.
func TestPlanAndVerifyIgnoreDesiredAbsenceTombstones(t *testing.T) {
	saved := profile.Plugins{
		Items:  []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}},
		Absent: []profile.Plugin{{ID: "acme", Source: "git", URL: "https://example.test/acme.git"}},
	}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}}}
	p := Provider{}
	plan := p.Plan(saved, current, 1, "4", "4", Semantics{ManageEnabled: true})
	if len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan = %#v, want no operations or skips for the tombstoned plugin", plan)
	}
	if !Verify(saved, current, Semantics{ManageEnabled: true}).OK {
		t.Fatal("verify unexpectedly failed because of a tombstoned plugin")
	}
}

func TestShellOwnedSemanticsIgnoreEnabledDrift(t *testing.T) {
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}}}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: false}}}
	semantics := Semantics{ManageEnabled: false}

	if got := Diff(saved, current, semantics); len(got) != 0 {
		t.Fatalf("diff = %#v", got)
	}
	if got := Verify(saved, current, semantics); !got.OK {
		t.Fatalf("verify = %#v", got)
	}
}

func TestShellOwnedSemanticsEmitNoEnableDisableOperations(t *testing.T) {
	p := Provider{UserDir: t.TempDir(), ProfileDir: t.TempDir()}
	// Missing local plugin: install/pin/validate/copy/rescan still planned,
	// but no enable/disable even though Enabled is true.
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "h", Enabled: true}}}
	current := profile.Plugins{}
	plan := p.Plan(saved, current, 4, "1.0", "1.0", Semantics{ManageEnabled: false})
	for _, operation := range plan.Operations {
		if operation.Action == "enable" || operation.Action == "disable" {
			t.Fatalf("shell-owned enablement must not be planned: %#v", plan.Operations)
		}
	}
	if len(plan.Operations) == 0 {
		t.Fatal("source reconstruction operations must still be planned")
	}
}

func TestLegacySemanticsPreserveEnablementBehavior(t *testing.T) {
	p := Provider{UserDir: t.TempDir(), ProfileDir: t.TempDir()}
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}}}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: false}}}
	semantics := Semantics{ManageEnabled: true}
	changes := Diff(saved, current, semantics)
	if len(changes) != 1 || !strings.Contains(changes[0].Summary, "enabled: false → true") {
		t.Fatalf("legacy diff = %#v", changes)
	}
	plan := p.Plan(saved, current, 3, "1.0", "1.0", semantics)
	foundEnable := false
	for _, operation := range plan.Operations {
		if operation.Action == "enable" {
			foundEnable = true
		}
	}
	if !foundEnable {
		t.Fatalf("legacy plan must emit enable: %#v", plan.Operations)
	}
	if got := Verify(saved, current, semantics); got.OK {
		t.Fatalf("legacy verify must fail on enabled drift: %#v", got)
	}
}

// --- PR 5 Task 30: safe Exact removal for third-party plugins ---

func pluginRemovalOp(plan model.RestorePlan, id string) *model.Operation {
	for i := range plan.Operations {
		if plan.Operations[i].Action == "remove" && plan.Operations[i].Resource == "plugin:"+id {
			return &plan.Operations[i]
		}
	}
	return nil
}

func TestPlanAdditiveNeverRemovesTombstonedPlugin(t *testing.T) {
	saved := profile.Plugins{Absent: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "hash"}}}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "hash"}}}
	// No PlanOptions at all: Additive is the zero value, matching every
	// existing caller that predates Exact removal.
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", Semantics{})
	if op := pluginRemovalOp(plan, "acme.weather"); op != nil {
		t.Fatalf("Operations = %#v, want no removal under Additive convergence", plan.Operations)
	}
}

func TestPlanExactNeverRemovesFirstPartyTombstone(t *testing.T) {
	saved := profile.Plugins{Absent: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin"}}}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", Semantics{}, PlanOptions{Exact: true})
	if op := pluginRemovalOp(plan, "omarchy.clock"); op != nil {
		t.Fatalf("Operations = %#v, want a first-party plugin never Exact-removed", plan.Operations)
	}
}

// TestPlanExactSkipsUnsafePluginIdentifier is the round-1 review blocker-4
// regression: the desired-present Items loop rejects !safeID(want.ID)
// before issuing any Omarchy command, but the Exact removal loop
// constructed omarchy plugin remove <id> --yes directly from a tombstoned
// ID with no such check. Destructive tombstones need at least the same
// safeID gate as every existing plugin operation.
func TestPlanExactSkipsUnsafePluginIdentifier(t *testing.T) {
	saved := profile.Plugins{Absent: []profile.Plugin{{ID: "../evil", Source: "local", Hash: "hash"}}}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "../evil", Source: "local", Hash: "hash"}}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", Semantics{}, PlanOptions{Exact: true})
	if len(plan.Operations) != 0 {
		t.Fatalf("Operations = %#v, want no removal for an unsafe plugin identifier", plan.Operations)
	}
	var found bool
	for _, skipped := range plan.Skipped {
		if skipped.Resource == "plugin:../evil" {
			found = true
			if !strings.Contains(skipped.Reason, "unsafe") {
				t.Fatalf("skip reason = %q, want it to explain the unsafe identifier", skipped.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("Skipped = %#v, want a visible skip for the unsafe identifier", plan.Skipped)
	}
}

func TestPlanExactSkipsChangedPluginProvenanceMismatch(t *testing.T) {
	saved := profile.Plugins{Absent: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "stale-hash"}}}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "different-hash"}}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", Semantics{}, PlanOptions{Exact: true})
	if op := pluginRemovalOp(plan, "acme.weather"); op != nil {
		t.Fatalf("Operations = %#v, want no removal for a changed/unowned local plugin", plan.Operations)
	}
	var found bool
	for _, skipped := range plan.Skipped {
		if skipped.Resource == "plugin:acme.weather" {
			found = true
			if !strings.Contains(skipped.Reason, "no longer matches") {
				t.Fatalf("skip reason = %q, want it to explain the provenance mismatch", skipped.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("Skipped = %#v, want a visible skip for the mismatched plugin", plan.Skipped)
	}
}

func TestPlanExactSkipsWhenTombstonedPluginAlreadyGone(t *testing.T) {
	saved := profile.Plugins{Absent: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "hash"}}}
	current := profile.Plugins{}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", Semantics{}, PlanOptions{Exact: true})
	if len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan = %#v, want no operation or skip: the tombstoned plugin is already gone", plan)
	}
}

func TestPlanExactRemovesTombstonedThirdPartyPlugin(t *testing.T) {
	saved := profile.Plugins{Absent: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "hash"}}}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "hash"}}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", Semantics{}, PlanOptions{Exact: true})
	op := pluginRemovalOp(plan, "acme.weather")
	if op == nil {
		t.Fatalf("Operations = %#v, want a removal operation for the tombstoned, provenance-matched plugin", plan.Operations)
	}
	if got, want := op.Command, []string{"omarchy", "plugin", "remove", "acme.weather", "--yes"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
	if op.Reversible {
		t.Fatalf("operation = %#v, want Reversible=false: removal is not guaranteed to be recoverable", op)
	}
	if op.Risk != model.RiskHigh {
		t.Fatalf("risk = %q, want high", op.Risk)
	}
}

func TestVerifyExactFailsWhenTombstonedPluginStillInstalled(t *testing.T) {
	saved := profile.Plugins{Absent: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "hash"}}}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "hash"}}}
	if Verify(saved, current, Semantics{}, VerifyOptions{Exact: true}).OK {
		t.Fatal("verification unexpectedly passed with the tombstoned plugin still installed under Exact")
	}
	if !Verify(saved, current, Semantics{}).OK {
		t.Fatal("Additive verification unexpectedly failed because of a tombstoned plugin still present")
	}
}

func TestVerifyExactPassesWhenTombstonedPluginAlreadyRemoved(t *testing.T) {
	saved := profile.Plugins{Absent: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "hash"}}}
	current := profile.Plugins{}
	if !Verify(saved, current, Semantics{}, VerifyOptions{Exact: true}).OK {
		t.Fatal("verification unexpectedly failed once the tombstoned plugin is gone")
	}
}
