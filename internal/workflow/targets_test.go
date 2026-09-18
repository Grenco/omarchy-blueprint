package workflow

import "testing"

func TestCaptureContextLookupFindsRecordedDecision(t *testing.T) {
	ctx := CaptureContext{Targets: map[string]CaptureDecision{
		"official:firefox": {Capture: false, Resolved: true},
	}}
	got, ok := ctx.Lookup("official:firefox")
	if !ok || got.Capture || !got.Resolved {
		t.Fatalf("Lookup() = %+v, %v, want explicit disabled, found", got, ok)
	}
}

func TestCaptureContextLookupMissingKeyIsNotFound(t *testing.T) {
	ctx := CaptureContext{Targets: map[string]CaptureDecision{}}
	if _, ok := ctx.Lookup("official:firefox"); ok {
		t.Fatal("Lookup() reported found for a key no orchestration ever recorded")
	}
}

func TestCaptureContextLookupNilTargetsIsNotFound(t *testing.T) {
	var ctx CaptureContext
	if _, ok := ctx.Lookup("official:firefox"); ok {
		t.Fatal("Lookup() reported found on a zero-value context")
	}
}

func TestCaptureContextRequireRejectsMissingKey(t *testing.T) {
	ctx := CaptureContext{Targets: map[string]CaptureDecision{}}
	if _, err := ctx.Require("official:firefox"); err == nil {
		t.Fatal("Require() must fail closed for a target with no recorded decision")
	}
}

func TestCaptureContextRequireRejectsUnresolvedDecision(t *testing.T) {
	ctx := CaptureContext{Targets: map[string]CaptureDecision{
		"official:firefox": DefaultCaptureDecision(),
	}}
	if _, err := ctx.Require("official:firefox"); err == nil {
		t.Fatal("Require() must reject the PR 2 compatibility default as write authority")
	}
}

func TestCaptureContextRequireAcceptsResolvedDecision(t *testing.T) {
	ctx := CaptureContext{Targets: map[string]CaptureDecision{
		"official:firefox": {Capture: false, Resolved: true},
	}}
	got, err := ctx.Require("official:firefox")
	if err != nil {
		t.Fatal(err)
	}
	if got.Capture {
		t.Fatalf("got %+v, want disabled", got)
	}
}

func TestDefaultCaptureDecisionIsEnabledUnresolved(t *testing.T) {
	got := DefaultCaptureDecision()
	if !got.Capture || got.Resolved {
		t.Fatalf("DefaultCaptureDecision() = %+v, want enabled/unresolved", got)
	}
}

func TestRestoreContextLookupFindsRecordedDecision(t *testing.T) {
	ctx := RestoreContext{Targets: map[string]RestoreDecision{
		"official:firefox": {Restore: false, Resolved: true},
	}}
	got, ok := ctx.Lookup("official:firefox")
	if !ok || got.Restore || !got.Resolved {
		t.Fatalf("Lookup() = %+v, %v, want explicit disabled, found", got, ok)
	}
}

func TestRestoreContextLookupMissingKeyIsNotFound(t *testing.T) {
	ctx := RestoreContext{Targets: map[string]RestoreDecision{}}
	if _, ok := ctx.Lookup("official:firefox"); ok {
		t.Fatal("Lookup() reported found for a key no orchestration ever recorded")
	}
}

func TestRestoreContextLookupNilTargetsIsNotFound(t *testing.T) {
	var ctx RestoreContext
	if _, ok := ctx.Lookup("official:firefox"); ok {
		t.Fatal("Lookup() reported found on a zero-value context")
	}
}

func TestRestoreContextRequireRejectsMissingKey(t *testing.T) {
	ctx := RestoreContext{Targets: map[string]RestoreDecision{}}
	if _, err := ctx.Require("official:firefox"); err == nil {
		t.Fatal("Require() must fail closed for a target with no recorded decision")
	}
}

func TestRestoreContextRequireRejectsUnresolvedDecision(t *testing.T) {
	ctx := RestoreContext{Targets: map[string]RestoreDecision{
		"official:firefox": DefaultRestoreDecision(),
	}}
	if _, err := ctx.Require("official:firefox"); err == nil {
		t.Fatal("Require() must reject the PR 2 compatibility default as write authority")
	}
}

func TestRestoreContextRequireAcceptsResolvedDecision(t *testing.T) {
	ctx := RestoreContext{Targets: map[string]RestoreDecision{
		"official:firefox": {Restore: false, Resolved: true},
	}}
	got, err := ctx.Require("official:firefox")
	if err != nil {
		t.Fatal(err)
	}
	if got.Restore {
		t.Fatalf("got %+v, want disabled", got)
	}
}

func TestDefaultRestoreDecisionIsEnabledUnresolved(t *testing.T) {
	got := DefaultRestoreDecision()
	if !got.Restore || got.Resolved {
		t.Fatalf("DefaultRestoreDecision() = %+v, want enabled/unresolved", got)
	}
}
