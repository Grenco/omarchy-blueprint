package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// approvalProvider is a transactional category whose live value the test
// controls per inspection, so it can change at any point between review,
// approval, and the end of Capture.
type approvalProvider struct {
	value      *string
	inspected  *int
	captured   *[]CaptureContext
	rolledBack *int
	// duringCapture runs as Capture reads the live value.
	duringCapture func()
}

func (approvalProvider) ID() string                 { return "hooks" }
func (approvalProvider) Captured(profile.Data) bool { return true }
func (p approvalProvider) InspectTargets(context.Context, profile.Data) ([]TargetInspection, error) {
	*p.inspected++
	return []TargetInspection{{
		Key: "post-update.d/setup", Label: "setup", Desired: TargetPresent, Current: TargetPresent,
		Fingerprint: *p.value, CaptureEligible: true,
		Capabilities: TargetCapabilities{SupportsCapture: true},
	}}, nil
}
func (p approvalProvider) Capture(_ context.Context, d *profile.Data, capCtx CaptureContext) (any, []model.Change, error) {
	*p.captured = append(*p.captured, capCtx)
	if p.duringCapture != nil {
		p.duringCapture()
	}
	d.Manifest.Capture.Hooks = true
	return *p.value, nil, nil
}

// The saved copy always differs from the live one, so the target is Update.
func (approvalProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return []model.Change{{Provider: "hooks", Kind: "hook", Name: "post-update.d/setup"}}, nil
}
func (approvalProvider) ChangeTargetKey(change model.Change) (string, bool) { return change.Name, true }
func (approvalProvider) CommitCapture() error                               { return nil }
func (approvalProvider) FinalizeCapture() error                             { return nil }
func (p approvalProvider) RollbackCapture() error {
	*p.rolledBack++
	return nil
}

type approvalFixture struct {
	session    *Session
	profileDir string
	value      string
	inspected  int
	captured   []CaptureContext
	rolledBack int
}

func newApprovalFixture(t *testing.T, duringCapture func(*approvalFixture)) *approvalFixture {
	t.Helper()
	f := &approvalFixture{profileDir: t.TempDir(), value: "hash-A"}
	stateHome := t.TempDir()
	if err := profile.Save(f.profileDir, profile.New("test", time.Now())); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{Runner: planRunner{}, Now: func() time.Time { return time.Unix(1, 0) }, StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: f.profileDir})
	if err != nil {
		t.Fatal(err)
	}
	provider := approvalProvider{value: &f.value, inspected: &f.inspected, captured: &f.captured, rolledBack: &f.rolledBack}
	if duringCapture != nil {
		provider.duringCapture = func() { duringCapture(f) }
	}
	if err := session.SetProviders([]Provider{provider}); err != nil {
		t.Fatal(err)
	}
	f.session = session
	return f
}

func (f *approvalFixture) review(t *testing.T) CaptureInspection {
	t.Helper()
	review, err := f.session.InspectCaptureMany(context.Background(), []string{"hooks"})
	if err != nil {
		t.Fatal(err)
	}
	if got := singleCaptureTarget(t, review, "hooks"); got.Outcome != CaptureOutcomeUpdate {
		t.Fatalf("review outcome = %s, want update", got.Outcome)
	}
	return review
}

func (f *approvalFixture) assertNothingSaved(t *testing.T) {
	t.Helper()
	saved, err := profile.Load(f.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Manifest.Capture.Hooks || f.session.Profile().Manifest.Capture.Hooks {
		t.Fatal("a refused approval must leave the profile untouched")
	}
}

// Update(A) → Update(B): the outcome is the same, but the value Capture
// would write is not the one the user approved.
func TestCaptureApprovedRefusesAValueThatChangedWithTheSameOutcome(t *testing.T) {
	f := newApprovalFixture(t, nil)
	approved := f.review(t)
	f.value = "hash-B"
	_, err := f.session.CaptureApproved(context.Background(), []string{"hooks"}, approved)
	var changed *CaptureReviewChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("CaptureApproved error = %v, want CaptureReviewChangedError", err)
	}
	if len(f.captured) != 0 {
		t.Fatalf("Capture ran %d times against a changed value", len(f.captured))
	}
	if got := singleCaptureTarget(t, changed.Fresh, "hooks"); got.Inspection.Fingerprint != "hash-B" {
		t.Fatalf("the recalculated review must carry the new value, got %q", got.Inspection.Fingerprint)
	}
	f.assertNothingSaved(t)
}

// A change after the approval comparison, while providers read live state
// during Capture, must not be saved on the strength of the old approval.
func TestCaptureApprovedRefusesAChangeAfterTheApprovalComparison(t *testing.T) {
	f := newApprovalFixture(t, func(f *approvalFixture) { f.value = "hash-B" })
	approved := f.review(t)
	_, err := f.session.CaptureApproved(context.Background(), []string{"hooks"}, approved)
	var changed *CaptureReviewChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("CaptureApproved error = %v, want CaptureReviewChangedError", err)
	}
	if f.rolledBack != 1 {
		t.Fatalf("staged Capture rolled back %d times, want 1", f.rolledBack)
	}
	f.assertNothingSaved(t)
}

// The inspection compared with the approval is the one Capture runs with:
// there is no further inspection between the comparison and Capture, only
// the post-staging re-check.
func TestCaptureApprovedCapturesWithTheComparedInspection(t *testing.T) {
	f := newApprovalFixture(t, nil)
	approved := f.review(t)
	f.inspected = 0
	if _, err := f.session.CaptureApproved(context.Background(), []string{"hooks"}, approved); err != nil {
		t.Fatal(err)
	}
	if f.inspected != 2 {
		t.Fatalf("approved Capture inspected %d times, want 2 (the compared authority and the post-staging re-check)", f.inspected)
	}
	decision, ok := f.captured[0].Lookup("post-update.d/setup")
	if len(f.captured) != 1 || !ok || !decision.Capture {
		t.Fatalf("Capture did not run with the approved decision: %#v", f.captured)
	}
	saved, err := profile.Load(f.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Manifest.Capture.Hooks {
		t.Fatal("an unchanged approval must save the capture")
	}
}
