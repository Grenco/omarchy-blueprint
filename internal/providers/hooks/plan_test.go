package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func planHook(t *testing.T, p Provider, path, body, mode string) profile.Hook {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	writeHook(t, filepath.Join(p.ProfileDir, "hooks/files", filepath.FromSlash(path)), body, 0o644)
	return profile.Hook{Path: path, Hash: hex.EncodeToString(sum[:]), Mode: mode}
}

func TestPlanCreatesMissingHookHighRisk(t *testing.T) {
	p, _, _ := hookProvider(t)
	saved := profile.Hooks{Items: []profile.Hook{planHook(t, p, "post-update.d/update-rust", "x", "0755")}}
	plan, err := p.Plan(saved, State{}, 5, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	op := plan.Operations[0]
	if op.Provider != "hooks" || op.Action != "write" || op.Resource != "hook:post-update.d/update-rust" || op.Risk != model.RiskHigh || !op.File.ExpectedMissing || op.File.Mode == nil || *op.File.Mode != 0o755 || len(op.Command) != 0 {
		t.Fatalf("operation = %#v", op)
	}
}

func TestPlanNoOpModeRepairConflictAndExtras(t *testing.T) {
	p, _, _ := hookProvider(t)
	savedHook := planHook(t, p, "post-boot", "desired", "0755")
	saved := profile.Hooks{Items: []profile.Hook{savedHook}}
	plan, err := p.Plan(saved, State{Items: []DetectedHook{{Path: "post-boot", Hash: savedHook.Hash, Mode: "0755"}}}, 5, "1", "2")
	if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan = %#v, err = %v", plan, err)
	}
	plan, err = p.Plan(saved, State{Items: []DetectedHook{{Path: "post-boot", Hash: savedHook.Hash, Mode: "0644"}}}, 5, "1", "2")
	if err != nil || len(plan.Operations) != 1 || plan.Operations[0].File.ExpectedMode == nil || !plan.Operations[0].File.Backup || !plan.Operations[0].Reversible {
		t.Fatalf("mode plan = %#v, err = %v", plan, err)
	}
	plan, err = p.Plan(saved, State{Items: []DetectedHook{{Path: "post-boot", Hash: "other", Mode: "0644"}, {Path: "extra", Hash: "extra", Mode: "0644"}}}, 5, "1", "2")
	if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 2 {
		t.Fatalf("conflict plan = %#v, err = %v", plan, err)
	}
}

func TestPlanRejectsInvalidMetadata(t *testing.T) {
	p, _, _ := hookProvider(t)
	if _, err := p.Plan(profile.Hooks{Items: []profile.Hook{{Path: "../bad", Hash: "bad", Mode: "0755"}}}, State{}, 5, "1", "2"); err == nil {
		t.Fatal("invalid metadata must fail")
	}
}

func TestPlanSkipsUnmanagedSymlinkAtDesiredPath(t *testing.T) {
	p, _, _ := hookProvider(t)
	savedHook := planHook(t, p, "post-boot", "desired", "0755")
	plan, err := p.Plan(profile.Hooks{Items: []profile.Hook{savedHook}}, State{Unmanaged: []UnmanagedHook{{Path: "post-boot", Target: "/external/hook"}}}, 5, "1", "2")
	if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "unmanaged symlink") {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestPlanSkipsChildOfUnmanagedSymlinkDirectory(t *testing.T) {
	p, user, _ := hookProvider(t)
	external := filepath.Join(t.TempDir(), "post-update.d")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(user, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(user, "post-update.d")); err != nil {
		t.Fatal(err)
	}
	savedHook := planHook(t, p, "post-update.d/update-rust", "desired", "0755")
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.Plan(profile.Hooks{Items: []profile.Hook{savedHook}}, current, 5, "1", "2")
	if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "hook directory post-update.d") {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if _, err := os.Stat(filepath.Join(external, "update-rust")); !os.IsNotExist(err) {
		t.Fatalf("external hook was created: %v", err)
	}
}

// TestPlanAndVerifyIgnoreDesiredAbsenceTombstones is Task 23's PR 3 safety
// gate: a Capture-produced desired-absence tombstone is write-only today --
// Capture writes it, but Restore's Plan/Verify never read Absent -- so it
// cannot cause an unexpected operation, skip, or verification failure until
// PR 4 activates Restore-side policy.
func TestPlanAndVerifyIgnoreDesiredAbsenceTombstones(t *testing.T) {
	p, _, _ := hookProvider(t)
	savedHook := planHook(t, p, "post-boot", "x", "0755")
	saved := profile.Hooks{
		Items:  []profile.Hook{savedHook},
		Absent: []profile.Hook{{Path: "post-update.d/removed", Hash: strings.Repeat("a", 64), Mode: "0644"}},
	}
	current := State{Items: []DetectedHook{{Path: "post-boot", Hash: savedHook.Hash, Mode: savedHook.Mode}}}
	plan, err := p.Plan(saved, current, 5, "1", "2")
	if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan=%#v err=%v, want no operations or skips for the tombstoned hook", plan, err)
	}
	if !Verify(saved, current).OK {
		t.Fatal("verify unexpectedly failed because of a tombstoned hook")
	}
}

// --- PR 5 Task 31: provenance-matched Exact deletion for Hooks ---

func hookDeleteOp(plan model.RestorePlan, path string) *model.Operation {
	for i := range plan.Operations {
		if plan.Operations[i].Action == "delete" && plan.Operations[i].Resource == "hook:"+path {
			return &plan.Operations[i]
		}
	}
	return nil
}

func TestPlanAdditiveNeverDeletesTombstonedHook(t *testing.T) {
	p, _, _ := hookProvider(t)
	saved := profile.Hooks{Absent: []profile.Hook{{Path: "post-update.d/removed", Hash: "hash", Mode: "0755"}}}
	current := State{Items: []DetectedHook{{Path: "post-update.d/removed", Hash: "hash", Mode: "0755"}}}
	// No PlanOptions at all: Additive is the zero value, matching every
	// existing caller that predates Exact deletion.
	plan, err := p.Plan(saved, current, 5, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	if op := hookDeleteOp(plan, "post-update.d/removed"); op != nil {
		t.Fatalf("Operations = %#v, want no deletion under Additive convergence", plan.Operations)
	}
	if len(plan.Skipped) != 0 {
		t.Fatalf("Skipped = %#v, want none under Additive: Absent is not \"extra\"", plan.Skipped)
	}
}

func TestPlanExactSkipsWhenTombstonedHookAlreadyMissing(t *testing.T) {
	p, _, _ := hookProvider(t)
	saved := profile.Hooks{Absent: []profile.Hook{{Path: "post-update.d/removed", Hash: "hash", Mode: "0755"}}}
	plan, err := p.Plan(saved, State{}, 5, "1", "2", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan = %#v, want no operation or skip: the tombstoned hook is already gone", plan)
	}
}

func TestPlanExactSkipsChangedHookContent(t *testing.T) {
	p, _, _ := hookProvider(t)
	saved := profile.Hooks{Absent: []profile.Hook{{Path: "post-update.d/removed", Hash: "stale-hash", Mode: "0755"}}}
	current := State{Items: []DetectedHook{{Path: "post-update.d/removed", Hash: "different-hash", Mode: "0755"}}}
	plan, err := p.Plan(saved, current, 5, "1", "2", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	if op := hookDeleteOp(plan, "post-update.d/removed"); op != nil {
		t.Fatalf("Operations = %#v, want no deletion for an independently changed hook", plan.Operations)
	}
	if len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "differs") {
		t.Fatalf("Skipped = %#v, want a visible skip explaining the content mismatch", plan.Skipped)
	}
}

func TestPlanExactSkipsUnmanagedSymlinkForTombstonedHook(t *testing.T) {
	p, _, _ := hookProvider(t)
	saved := profile.Hooks{Absent: []profile.Hook{{Path: "post-boot", Hash: "hash", Mode: "0755"}}}
	current := State{Unmanaged: []UnmanagedHook{{Path: "post-boot", Target: "/external/hook"}}}
	plan, err := p.Plan(saved, current, 5, "1", "2", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	if op := hookDeleteOp(plan, "post-boot"); op != nil {
		t.Fatalf("Operations = %#v, want no deletion of an unmanaged symlink", plan.Operations)
	}
	if len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "unmanaged symlink") {
		t.Fatalf("Skipped = %#v, want a visible skip explaining the unmanaged symlink", plan.Skipped)
	}
}

func TestPlanExactDeletesProvenanceMatchedTombstonedHook(t *testing.T) {
	p, _, _ := hookProvider(t)
	saved := profile.Hooks{Absent: []profile.Hook{{Path: "post-update.d/removed", Hash: strings.Repeat("a", 64), Mode: "0755"}}}
	current := State{Items: []DetectedHook{{Path: "post-update.d/removed", Hash: strings.Repeat("a", 64), Mode: "0755"}}}
	plan, err := p.Plan(saved, current, 5, "1", "2", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	op := hookDeleteOp(plan, "post-update.d/removed")
	if op == nil {
		t.Fatalf("Operations = %#v, want a deletion for the tombstoned, provenance-matched hook", plan.Operations)
	}
	if op.Delete == nil || op.Delete.Destination != filepath.Join(p.UserDir, "post-update.d", "removed") {
		t.Fatalf("operation = %#v", op)
	}
	if !op.Delete.Backup || !op.Delete.RejectSymlinkParents {
		t.Fatalf("operation = %#v, want Backup and RejectSymlinkParents set", op)
	}
	if op.Delete.ExpectedExisting == nil || op.Delete.ExpectedExisting.Hash != strings.Repeat("a", 64) || op.Delete.ExpectedExisting.Mode != 0o755 {
		t.Fatalf("precondition = %#v", op.Delete.ExpectedExisting)
	}
	if op.Risk != model.RiskHigh || !op.Reversible {
		t.Fatalf("operation = %#v, want RiskHigh and Reversible (backup taken)", op)
	}
	if len(plan.Skipped) != 0 {
		t.Fatalf("Skipped = %#v, want none", plan.Skipped)
	}
}

func TestPlanExactSkipsReservedInboundLinkForTombstonedHook(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	p := Provider{
		UserDir:    filepath.Join(home, ".config", "omarchy", "hooks"),
		ProfileDir: filepath.Join(root, "profile"),
		HomeDir:    home,
		Resources: profile.Resources{
			Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "copy"}},
			Links: []profile.ResourceLink{{Source: "~/.config/omarchy/hooks/post-boot", TargetResource: "dotfiles", Target: "hooks/post-boot", Origin: "inbound"}},
		},
	}
	saved := profile.Hooks{Absent: []profile.Hook{{Path: "post-boot", Hash: strings.Repeat("a", 64), Mode: "0755"}}}
	current := State{Items: []DetectedHook{{Path: "post-boot", Hash: strings.Repeat("a", 64), Mode: "0755"}}}
	plan, err := p.Plan(saved, current, 5, "1", "2", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	if op := hookDeleteOp(plan, "post-boot"); op != nil {
		t.Fatalf("Operations = %#v, want no deletion of a Resources-reserved path", plan.Operations)
	}
	if len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "reserved") {
		t.Fatalf("Skipped = %#v, want a visible skip explaining the reservation", plan.Skipped)
	}
}

func TestVerifyExactFailsWhenTombstonedHookStillPresent(t *testing.T) {
	saved := profile.Hooks{Absent: []profile.Hook{{Path: "post-update.d/removed", Hash: "hash", Mode: "0755"}}}
	current := State{Items: []DetectedHook{{Path: "post-update.d/removed", Hash: "hash", Mode: "0755"}}}
	if Verify(saved, current, VerifyOptions{Exact: true}).OK {
		t.Fatal("verification unexpectedly passed with the tombstoned hook still present under Exact")
	}
	if !Verify(saved, current).OK {
		t.Fatal("Additive verification unexpectedly failed because of a tombstoned hook still present")
	}
}

func TestVerifyExactPassesWhenTombstonedHookAlreadyDeleted(t *testing.T) {
	saved := profile.Hooks{Absent: []profile.Hook{{Path: "post-update.d/removed", Hash: "hash", Mode: "0755"}}}
	if !Verify(saved, State{}, VerifyOptions{Exact: true}).OK {
		t.Fatal("verification unexpectedly failed once the tombstoned hook is gone")
	}
}

func TestPlanDefersReservedInboundResourceLink(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	p := Provider{
		UserDir:    filepath.Join(home, ".config", "omarchy", "hooks"),
		ProfileDir: filepath.Join(root, "profile"),
		HomeDir:    home,
		Resources: profile.Resources{
			Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "copy"}},
			Links: []profile.ResourceLink{{Source: "~/.config/omarchy/hooks/post-boot", TargetResource: "dotfiles", Target: "hooks/post-boot", Origin: "inbound"}},
		},
	}
	savedHook := planHook(t, p, "post-boot", "legacy", "0755")
	plan, err := p.Plan(profile.Hooks{Items: []profile.Hook{savedHook}}, State{}, 5, "1", "2")
	if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}
