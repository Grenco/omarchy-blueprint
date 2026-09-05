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
