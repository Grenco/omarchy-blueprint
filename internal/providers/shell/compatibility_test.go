package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestRestoreCompatibilitySupportedRecordedAndCurrentSchemas(t *testing.T) {
	p, saved, f := captureCustomized(t)
	f.writeBaseline(strings.Replace(defaultShellJSON, `"screensaver": 150`, `"screensaver": 200`, 1))
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.RestoreCompatibility(saved, current, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.CompatibilitySupported || got.Authority != model.CompatibilityUnchanged || len(got.Evidence) == 0 || len(got.Findings) != 0 {
		t.Fatalf("supported Shell merge protocol = %+v", got)
	}
	// The normal semantic merge/Verify path is unchanged by PR B.
	if _, err := p.Plan(saved, current, 13, "4.0", "5.0", MergeOptions{}); err != nil {
		t.Fatalf("supported planning changed: %v", err)
	}
}

func TestRestoreCompatibilityUnsupportedRecordedSchemaBlocks(t *testing.T) {
	p, saved, _ := captureCustomized(t)
	desiredRaw := strings.Replace(customizedShellJSON, `"version": 1`, `"version": 2`, 1)
	baselineRaw := strings.Replace(defaultShellJSON, `"version": 1`, `"version": 2`, 1)
	desired, err := ParseDocument([]byte(desiredRaw))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := ParseDocument([]byte(baselineRaw))
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{"shell.json": desiredRaw, "baseline.json": baselineRaw} {
		if err := os.WriteFile(filepath.Join(p.ProfileDir, "shell", name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	saved.Version, saved.Hash, saved.BaselineHash = 2, desired.Hash, baseline.Hash
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.RestoreCompatibility(saved, current, true)
	if err != nil || got.State != model.CompatibilityIncompatible || got.Authority != model.CompatibilityBlocked || len(got.Findings) != 1 || got.Findings[0].Code != "shell.schema.unsupported" || got.Findings[0].Target != "state" {
		t.Fatalf("unsupported captured schema = %+v err=%v", got, err)
	}
}

func TestRestoreCompatibilityUnsupportedCurrentSchemaBlocks(t *testing.T) {
	for _, change := range []struct {
		name  string
		write func(shellFixture)
	}{
		{"baseline", func(f shellFixture) { f.writeBaseline(`{"version":2}`) }},
		{"user", func(f shellFixture) { f.writeUser(`{"version":2}`) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			p, saved, f := captureCustomized(t)
			change.write(f)
			current, err := p.Detect()
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.RestoreCompatibility(saved, current, true)
			if err != nil || got.State != model.CompatibilityIncompatible || got.Authority != model.CompatibilityBlocked || got.Findings[0].Code != "shell.schema.unsupported" {
				t.Fatalf("unsupported target schema = %+v err=%v", got, err)
			}
			plan, err := p.Plan(saved, current, 13, "4.0", "5.0", MergeOptions{})
			if err != nil || len(plan.Operations) != 0 {
				t.Fatalf("unsupported schema planned mutation: %+v err=%v", plan, err)
			}
		})
	}
}

func TestRestoreCompatibilityCorruptSnapshotRemainsError(t *testing.T) {
	p, saved, _ := captureCustomized(t)
	if err := os.WriteFile(filepath.Join(p.ProfileDir, "shell", "shell.json"), []byte(`{"version":1,"bar":{"id":"changed"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.RestoreCompatibility(saved, current, true); err == nil {
		t.Fatal("corrupt trusted snapshot must be an error, not Unknown")
	}
}

func TestRestoreCompatibilitySkipAndNoIntentAreNotApplicable(t *testing.T) {
	p, saved, _ := captureCustomized(t)
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		saved profile.Shell
		apply bool
	}{{saved, false}, {profile.Shell{Version: SupportedVersion}, true}} {
		got, err := p.RestoreCompatibility(tc.saved, current, tc.apply)
		if err != nil || got.Applies || got.State != "" || len(got.Findings) != 0 || got.Authority != model.CompatibilityUnchanged {
			t.Fatalf("no effective Shell intent = %+v err=%v", got, err)
		}
	}
}

func TestRestoreCompatibilityUsesDetectedStateWithoutSecondTargetProbe(t *testing.T) {
	p, saved, f := captureCustomized(t)
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(p.ProfileDir, "shell", "shell.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Assessment uses the captured State, not another live target read.
	if err := os.Remove(f.user); err != nil {
		t.Fatal(err)
	}
	got, err := p.RestoreCompatibility(saved, current, true)
	if err != nil || got.State != model.CompatibilitySupported {
		t.Fatalf("reuse detected state = %+v err=%v", got, err)
	}
	after, err := os.ReadFile(filepath.Join(p.ProfileDir, "shell", "shell.json"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("compatibility changed profile snapshot: %v", err)
	}
}
