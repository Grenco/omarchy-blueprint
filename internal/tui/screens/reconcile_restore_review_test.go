package screens

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/providers/config"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestSyncRestoreReviewUsesSelectedMachinesSavedForceDefaults(t *testing.T) {
	provider := &countedRestoreProvider{}
	screen, _ := readRestoreScreenWithState(t, provider)
	session := screen.session
	data, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	data.Machines.Items = []profile.Machine{{Name: "laptop", RestoreConflicts: policy.ConflictForce, RestoreConvergence: policy.ConvergenceExact}}
	if err := profile.Save(session.ProfileDir(), data); err != nil {
		t.Fatal(err)
	}
	if err := session.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := session.UseMachine(context.Background(), "laptop"); err != nil {
		t.Fatal(err)
	}
	e := &reconcileExec{session: session}
	review := e.restoreReview(context.Background(), model.RestorePlan{})
	machineName, conflicts, _, convergence, _, defaults, _ := review.runSettings()
	if machineName != "laptop" || conflicts != "Force" || convergence != "Exact" || defaults != "Machine defaults" {
		t.Fatalf("wrong selected-machine review: %s / %s / %s / %s", machineName, conflicts, convergence, defaults)
	}
	if !strings.Contains(review.confirmation(), "not assessed") || provider.calls.Load() != 0 {
		t.Fatal("review invented a count or re-observed live state")
	}
}

func TestSyncFreshRestoreReviewNamesMachineAndDoesNotInventForceCount(t *testing.T) {
	root, savedRoot := t.TempDir(), t.TempDir()
	path := "app/config"
	for _, fixture := range []struct{ root, contents string }{{root, "unknown live contents"}, {filepath.Join(savedRoot, "config/files"), "desired contents"}} {
		file := filepath.Join(fixture.root, path)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(fixture.contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hash := sha256.Sum256([]byte("desired contents"))
	provider := config.Provider{UserRoot: root, BaselineRoot: t.TempDir(), ProfileDir: savedRoot}
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: hex.EncodeToString(hash[:])}}}
	scan := config.ScanSummary{Candidates: []config.Candidate{{Path: path, Classification: config.ConfigAdded, UserHash: "unknown"}}}
	safe, err := provider.PlanOverlay(saved, scan, 8, "old", "new")
	if err != nil {
		t.Fatal(err)
	}
	forced, err := provider.PlanOverlay(saved, scan, 8, "old", "new", config.PlanOptions{Force: true})
	if err != nil || len(safe.Operations) != 0 || len(forced.Operations) != 1 || !forced.Operations[0].File.ReplaceExisting {
		t.Fatalf("fixture must include an actual Force-only replacement: safe=%v forced=%v err=%v", safe, forced, err)
	}
	options := policy.RestoreOptions{Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceExact}
	f := &reconcileFixture{execute: func(ctx context.Context, req workflow.SyncExecutionRequest) (workflow.SyncExecutionResult, error) {
		return workflow.SyncExecutionResult{}, req.ApproveRestore(ctx, forced)
	}}
	var output bytes.Buffer
	e := &reconcileExec{ctx: context.Background(), session: f, in: strings.NewReader("no\n"), out: &output,
		request: workflow.SyncExecutionRequest{ReviewedPlan: syncengine.Plan{Identity: syncengine.Identity{Machine: "laptop"}}, RestoreOptions: &options}}
	e.Run()
	text := output.String()
	for _, want := range []string{"laptop", "Force", "Exact", "not assessed", "high-risk"} {
		if !strings.Contains(text, want) {
			t.Errorf("fresh approval lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Profile defaults") || strings.Contains(text, "0 forced") {
		t.Errorf("fresh approval misrepresents context:\n%s", text)
	}
	if e.err == nil {
		t.Fatal("declined approval was accepted")
	}
}
