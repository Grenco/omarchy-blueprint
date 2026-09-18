package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type captureInspectionTestProvider struct {
	id      string
	targets []TargetInspection
}

func (p captureInspectionTestProvider) ID() string               { return p.id }
func (captureInspectionTestProvider) Captured(profile.Data) bool { return true }
func (p captureInspectionTestProvider) InspectTargets(context.Context, profile.Data) ([]TargetInspection, error) {
	return p.targets, nil
}
func (captureInspectionTestProvider) Capture(context.Context, *profile.Data, CaptureContext) (any, []model.Change, error) {
	panic("InspectCapture must not call Capture")
}
func (captureInspectionTestProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return nil, nil
}

func TestInspectCaptureReportsAddForNewlyPresentTarget(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
		{Key: "official:firefox", CaptureEligible: true, Current: TargetPresent, Desired: TargetUnknown},
	}}})

	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := singleCaptureTarget(t, inspection, "packages")
	if got.Outcome != CaptureOutcomeAdd {
		t.Fatalf("Outcome = %s, want %s", got.Outcome, CaptureOutcomeAdd)
	}
}

func TestInspectCaptureReportsUpdateForAlreadyTrackedPresentTarget(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
		{Key: "official:firefox", CaptureEligible: true, Current: TargetPresent, Desired: TargetPresent},
	}}})

	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := singleCaptureTarget(t, inspection, "packages")
	if got.Outcome != CaptureOutcomeUpdate {
		t.Fatalf("Outcome = %s, want %s", got.Outcome, CaptureOutcomeUpdate)
	}
}

func TestInspectCaptureReportsAbsentForTrackedTargetNoLongerPresent(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
		{
			Key: "official:firefox", CaptureEligible: true, Current: TargetAbsent, Desired: TargetPresent,
			Capabilities: TargetCapabilities{SupportsDesiredAbsence: true},
		},
	}}})

	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := singleCaptureTarget(t, inspection, "packages")
	if got.Outcome != CaptureOutcomeAbsent {
		t.Fatalf("Outcome = %s, want %s", got.Outcome, CaptureOutcomeAbsent)
	}
}

// A provider that cannot express desired absence (e.g. Resources) must never
// report Absent for a missing-but-desired target: with no way to record the
// absence, Capture leaves the previously desired state untouched.
func TestInspectCapturePreservesMissingTargetWhenProviderCannotExpressAbsence(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "resources", targets: []TargetInspection{
		{
			Key: "resource:dotfiles", CaptureEligible: true, Current: TargetAbsent, Desired: TargetPresent,
			Capabilities: TargetCapabilities{SupportsDesiredAbsence: false},
		},
	}}})

	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := singleCaptureTarget(t, inspection, "resources")
	if got.Outcome != CaptureOutcomePreserve {
		t.Fatalf("Outcome = %s, want %s", got.Outcome, CaptureOutcomePreserve)
	}
}

func TestInspectCaptureReportsNoopForUntrackedAbsentTarget(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
		{Key: "official:firefox", CaptureEligible: true, Current: TargetAbsent, Desired: TargetUnknown},
	}}})

	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := singleCaptureTarget(t, inspection, "packages")
	if got.Outcome != CaptureOutcomeNoop {
		t.Fatalf("Outcome = %s, want %s", got.Outcome, CaptureOutcomeNoop)
	}
}

func TestInspectCaptureReportsBlockedRegardlessOfPresence(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
		{Key: "official:firefox", CaptureEligible: false, Current: TargetPresent, Desired: TargetPresent, SafetyReason: "hardware profile"},
	}}})

	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := singleCaptureTarget(t, inspection, "packages")
	if got.Outcome != CaptureOutcomeBlocked {
		t.Fatalf("Outcome = %s, want %s", got.Outcome, CaptureOutcomeBlocked)
	}
}

func TestInspectCapturePreservesWhenCaptureDecisionIsDisabled(t *testing.T) {
	if got := captureOutcome(TargetInspection{CaptureEligible: true, Current: TargetPresent, Desired: TargetPresent}, CaptureDecision{Capture: false, Resolved: true}); got != CaptureOutcomePreserve {
		t.Fatalf("Outcome = %s, want %s", got, CaptureOutcomePreserve)
	}
}

func TestInspectCaptureDoesNotPersistOrMutateProfile(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
		{Key: "official:firefox", CaptureEligible: true, Current: TargetPresent, Desired: TargetUnknown},
	}}})
	before, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := session.InspectCapture(context.Background(), ""); err != nil {
		t.Fatal(err)
	}

	after, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Packages.Official) != len(before.Packages.Official) {
		t.Fatalf("InspectCapture wrote to the profile: before=%#v after=%#v", before.Packages, after.Packages)
	}
	if len(session.Profile().Packages.Official) != 0 {
		t.Fatalf("InspectCapture mutated the in-memory session profile: %#v", session.Profile().Packages)
	}
}

func TestInspectCaptureSkipsCategoriesTheProviderReportsNoTargetsFor(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "packages"}})

	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := inspection.Categories["packages"]; ok {
		t.Fatalf("Categories = %#v, want packages omitted when no targets are reported", inspection.Categories)
	}
}

func newInspectionSession(t *testing.T) *Session {
	t.Helper()
	profileDir, stateHome := t.TempDir(), t.TempDir()
	if err := profile.Save(profileDir, profile.New("test", time.Now())); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func singleCaptureTarget(t *testing.T, inspection CaptureInspection, category string) CaptureTarget {
	t.Helper()
	items, ok := inspection.Categories[category]
	if !ok || len(items) != 1 {
		t.Fatalf("Categories[%q] = %#v, want exactly one target", category, items)
	}
	return items[0]
}
