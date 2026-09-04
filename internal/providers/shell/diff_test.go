package shell

import (
	"os"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func captureIntent(t *testing.T, desired string) (shellFixture, Provider, profile.Shell) {
	t.Helper()
	f := newShellFixture(t)
	f.writeUser(desired)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.Capture(state)
	if err != nil {
		t.Fatal(err)
	}
	return f, p, saved
}

func sourceLock(value int) string {
	return strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": `+string(rune('0'+value/100))+`00`, 1)
}

func TestDiffEmptySourceIntentIgnoresTargetCustomization(t *testing.T) {
	f := newShellFixture(t)
	f.writeUser(strings.Replace(defaultShellJSON, `"position": "top"`, `"position": "left"`, 1))
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	changes, err := p.Diff(profile.Shell{Version: 1}, current)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%#v err=%v", changes, err)
	}
}

func TestDiffReportsSourceOnlyIntent(t *testing.T) {
	f, p, saved := captureIntent(t, strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 600`, 1))
	f.writeUser(defaultShellJSON)
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	changes, err := p.Diff(saved, current)
	if err != nil || len(changes) != 1 || changes[0].Name != "idle.lock" || changes[0].Type != model.ChangeModify {
		t.Fatalf("changes=%#v err=%v", changes, err)
	}
}

func TestDiffIgnoresTargetOnlyFieldChange(t *testing.T) {
	f, p, saved := captureIntent(t, strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 600`, 1))
	target := strings.Replace(defaultShellJSON, `"position": "top"`, `"position": "left"`, 1)
	target = strings.Replace(target, `"lock": 300`, `"lock": 600`, 1)
	f.writeUser(target)
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	changes, err := p.Diff(saved, current)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%#v err=%v", changes, err)
	}
}

func TestDiffReportsIndependentConflict(t *testing.T) {
	f, p, saved := captureIntent(t, strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 600`, 1))
	f.writeUser(strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 900`, 1))
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	changes, err := p.Diff(saved, current)
	if err != nil || len(changes) != 1 || changes[0].Type != model.ChangeWarn || !strings.Contains(changes[0].Summary, "preserved") {
		t.Fatalf("changes=%#v err=%v", changes, err)
	}
}

func TestVerifyAllowsTargetOnlyCustomization(t *testing.T) {
	f := newShellFixture(t)
	f.writeUser(strings.Replace(defaultShellJSON, `"position": "top"`, `"position": "left"`, 1))
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Verify(profile.Shell{Version: 1}, current)
	if err != nil || !result.OK {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestVerifyFailsUnsatisfiedSourceIntentAndConflict(t *testing.T) {
	f, p, saved := captureIntent(t, strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 600`, 1))
	for _, target := range []string{defaultShellJSON, strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 900`, 1)} {
		f.writeUser(target)
		current, err := p.Detect()
		if err != nil {
			t.Fatal(err)
		}
		result, err := p.Verify(saved, current)
		if err != nil || result.OK || len(result.Missing) != 1 || result.Missing[0] != "shell:idle.lock" {
			t.Fatalf("target=%s result=%#v err=%v", target, result, err)
		}
	}
}

func TestDiffKeepsChangedCurrentBaselineForUntouchedSourceField(t *testing.T) {
	f, p, saved := captureIntent(t, strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 600`, 1))
	currentBaseline := strings.Replace(defaultShellJSON, `"screensaver": 150`, `"screensaver": 200`, 1)
	f.writeBaseline(currentBaseline)
	if err := os.Remove(f.user); err != nil {
		t.Fatal(err)
	}
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := p.Analyze(saved, current, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	idle := analysis.Value["idle"].(map[string]any)
	if canonicalValue(idle["screensaver"]) != "200" || len(analysis.Applied) != 1 || pathName(analysis.Applied[0].Path) != "idle.lock" {
		t.Fatalf("analysis=%#v", analysis)
	}
}
