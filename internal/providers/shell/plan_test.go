package shell

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func captureCustomized(t *testing.T) (Provider, profile.Shell, shellFixture) {
	t.Helper()
	f := newShellFixture(t)
	f.writeUser(customizedShellJSON)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.Capture(state)
	if err != nil {
		t.Fatal(err)
	}
	return p, saved, f
}

func TestPlanNoOpsWhenDesiredShellAlreadyMatches(t *testing.T) {
	p, saved, _ := captureCustomized(t)
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.Plan(saved, current, 4, "1.0", "1.0", MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanCreatesMissingUserShellAndRestarts(t *testing.T) {
	p, saved, f := captureCustomized(t)
	if err := os.Remove(f.user); err != nil {
		t.Fatal(err)
	}
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.Plan(saved, current, 4, "1.0", "1.0", MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	write := plan.Operations[0]
	if write.ID != "shell.write" || write.File == nil || !write.File.ExpectedMissing || write.File.ExpectedHash != "" {
		t.Fatalf("write = %#v", write)
	}
	if write.File.Backup {
		t.Fatal("missing-target write must not request a backup")
	}
	restart := plan.Operations[1]
	if got := restart.Command; !reflect.DeepEqual(got, []string{"omarchy-restart-shell"}) {
		t.Fatalf("restart command = %#v", got)
	}
	if !reflect.DeepEqual(restart.DependsOn, []string{"shell.write"}) {
		t.Fatalf("restart dependencies = %#v", restart.DependsOn)
	}
}

func TestPlanReplacesCurrentDefaultShellWithBackup(t *testing.T) {
	p, saved, f := captureCustomized(t)
	// The machine currently uses the Omarchy default.
	f.writeUser(defaultShellJSON)
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.Plan(saved, current, 4, "1.0", "1.0", MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	write := plan.Operations[0]
	if write.File == nil || write.File.ExpectedMissing || write.File.ExpectedHash == "" {
		t.Fatalf("replace write = %#v", write.File)
	}
	if !write.File.Backup || !write.Reversible {
		t.Fatalf("replacement must be backed up: %#v", write)
	}
	if restart := plan.Operations[1].ID; restart != "shell.restart" {
		t.Fatalf("restart = %#v", plan.Operations[1])
	}
}

func TestPlanAllowsBaselineContentChangeWhenVersionMatches(t *testing.T) {
	p, saved, f := captureCustomized(t)
	// Omarchy updates its baseline content but keeps version 1; this must
	// not block restoration because the user document is authoritative.
	f.writeBaseline(strings.Replace(defaultShellJSON, `"screensaver": 150`, `"screensaver": 999`, 1))
	// The machine is running the (updated) Omarchy default.
	f.writeUser(strings.Replace(defaultShellJSON, `"screensaver": 150`, `"screensaver": 999`, 1))
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if current.BaselineHash == saved.BaselineHash {
		t.Fatal("fixture must actually change the baseline")
	}
	plan, err := p.Plan(saved, current, 4, "1.0", "1.0", MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 || len(plan.Skipped) != 0 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanSkipsDifferentUserCustomization(t *testing.T) {
	p, saved, f := captureCustomized(t)
	different := strings.Replace(customizedShellJSON, `"lock": 900`, `"lock": 777`, 1)
	f.writeUser(different)
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.Plan(saved, current, 4, "1.0", "1.0", MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v; user drift must never be overwritten", plan.Operations)
	}
	if len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "keeping the current value") {
		t.Fatalf("skipped = %#v", plan.Skipped)
	}
}

func TestPlanSkipsShellVersionMismatchAsMigrationRequired(t *testing.T) {
	p, saved, f := captureCustomized(t)
	f.writeUser(`{"version":2}`)
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.Plan(saved, current, 4, "1.0", "1.0", MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 || len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "Shell schema changed; migration required") {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanDefaultDesiredStateDoesNotDeleteExtraCustomization(t *testing.T) {
	p, _, f := captureCustomized(t)
	// Profile now wants the default; machine keeps a different customization.
	saved := profile.Shell{Version: 1, BaselineHash: hashFixture(t, f.baseline)}
	f.writeUser(customizedShellJSON)
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.Plan(saved, current, 4, "1.0", "1.0", MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v; removal is disabled", plan.Operations)
	}
	if len(plan.Skipped) != 0 {
		t.Fatalf("skipped = %#v", plan.Skipped)
	}
}

func TestPlanUsesExactCapturedRawHashForFileWriteSource(t *testing.T) {
	p, saved, f := captureCustomized(t)
	if err := os.Remove(f.user); err != nil {
		t.Fatal(err)
	}
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.Plan(saved, current, 4, "1.0", "1.0", MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	desired, err := ReadDocument(filepath.Join(f.profile, "shell", "shell.json"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Operations[0].File.SourceHash != desired.RawHash {
		t.Fatalf("source hash = %q, want exact captured raw hash %q", plan.Operations[0].File.SourceHash, desired.RawHash)
	}
	if plan.Operations[0].Risk != model.RiskMedium {
		t.Fatalf("risk = %s", plan.Operations[0].Risk)
	}
}
