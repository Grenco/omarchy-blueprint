package workflow

import "testing"

func TestCaptureContextDecisionExplicitDisabled(t *testing.T) {
	ctx := CaptureContext{Targets: map[string]CaptureDecision{
		"official:firefox": {Capture: false, Resolved: true},
	}}
	got := ctx.Decision("official:firefox")
	if got.Capture || !got.Resolved {
		t.Fatalf("Decision() = %+v, want explicit disabled", got)
	}
}

func TestCaptureContextDecisionMissingKeyDefaultsEnabledUnresolved(t *testing.T) {
	ctx := CaptureContext{Targets: map[string]CaptureDecision{}}
	got := ctx.Decision("official:firefox")
	if !got.Capture || got.Resolved {
		t.Fatalf("Decision() = %+v, want default enabled/unresolved", got)
	}
}

func TestCaptureContextDecisionNilTargetsDefaultsEnabledUnresolved(t *testing.T) {
	var ctx CaptureContext
	got := ctx.Decision("official:firefox")
	if !got.Capture || got.Resolved {
		t.Fatalf("Decision() = %+v, want default enabled/unresolved", got)
	}
}

func TestRestoreContextDecisionExplicitDisabled(t *testing.T) {
	ctx := RestoreContext{Targets: map[string]RestoreDecision{
		"official:firefox": {Restore: false, Resolved: true},
	}}
	got := ctx.Decision("official:firefox")
	if got.Restore || !got.Resolved {
		t.Fatalf("Decision() = %+v, want explicit disabled", got)
	}
}

func TestRestoreContextDecisionMissingKeyDefaultsEnabledUnresolved(t *testing.T) {
	ctx := RestoreContext{Targets: map[string]RestoreDecision{}}
	got := ctx.Decision("official:firefox")
	if !got.Restore || got.Resolved {
		t.Fatalf("Decision() = %+v, want default enabled/unresolved", got)
	}
}

func TestRestoreContextDecisionNilTargetsDefaultsEnabledUnresolved(t *testing.T) {
	var ctx RestoreContext
	got := ctx.Decision("official:firefox")
	if !got.Restore || got.Resolved {
		t.Fatalf("Decision() = %+v, want default enabled/unresolved", got)
	}
}

