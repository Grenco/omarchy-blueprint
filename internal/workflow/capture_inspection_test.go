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

// TestInspectCaptureReportsStopManagingForPresentTargetWithNoActionableUpdate
// is a regression for a round-3 review finding on PR 3: a present and
// desired-present target normally means real Capture would write a
// genuinely fresh value (Update), but a provider can mark a classification
// as never actually producing one (Config's UnchangedBaseline/
// HistoricalBaseline: baseline-derived, not real user customization). The
// preview must then agree with what Capture actually does -- drop the
// stale value, not "update" it to anything -- rather than reporting the
// generic Update.
func TestInspectCaptureReportsStopManagingForPresentTargetWithNoActionableUpdate(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "config", targets: []TargetInspection{
		{
			Key: ".config/hypr/bindings.lua", CaptureEligible: true, Current: TargetPresent, Desired: TargetPresent,
			Capabilities: TargetCapabilities{NoActionableUpdate: true},
		},
	}}})

	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := singleCaptureTarget(t, inspection, "config")
	if got.Outcome != CaptureOutcomeStopManaging {
		t.Fatalf("Outcome = %s, want %s", got.Outcome, CaptureOutcomeStopManaging)
	}
}

// TestInspectCapturePreservesNoActionableUpdateTargetWhenCaptureDisabled
// confirms the paired disabled transition still Preserves, exactly like any
// other target: NoActionableUpdate only changes what the *enabled*
// transition reports.
func TestInspectCapturePreservesNoActionableUpdateTargetWhenCaptureDisabled(t *testing.T) {
	got := captureOutcome(TargetInspection{
		CaptureEligible: true, Current: TargetPresent, Desired: TargetPresent,
		Capabilities: TargetCapabilities{NoActionableUpdate: true},
	}, CaptureDecision{Capture: false, Resolved: true})
	if got != CaptureOutcomePreserve {
		t.Fatalf("Outcome = %s, want %s", got, CaptureOutcomePreserve)
	}
}

// TestInspectCaptureReportsStopManagingForDesiredAbsentTargetWithNoActionableUpdate
// is a regression for a round-3 review finding on PR 3: NoActionableUpdate
// was only checked inside the present-and-desired-present switch case, so a
// target that is Current=Present but Desired=Absent (a saved deletion
// tombstone whose file reappeared matching the baseline) fell through to
// the generic Add case ("add it back") instead. Real Capture treats a
// baseline-derived classification's stale desired state the same way
// whether it was a captured value or a deletion tombstone: enabling Capture
// drops it entirely, since matching the baseline again means the deletion
// intent is no longer meaningful customization either.
func TestInspectCaptureReportsStopManagingForDesiredAbsentTargetWithNoActionableUpdate(t *testing.T) {
	got := captureOutcome(TargetInspection{
		CaptureEligible: true, Current: TargetPresent, Desired: TargetAbsent,
		Capabilities: TargetCapabilities{NoActionableUpdate: true},
	}, CaptureDecision{Capture: true, Resolved: true})
	if got != CaptureOutcomeStopManaging {
		t.Fatalf("Outcome = %s, want %s", got, CaptureOutcomeStopManaging)
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

// A provider that cannot express desired absence but preserves a missing
// target anyway (e.g. Resources: no way to tell "gone" from "not yet
// restored" apart) must never report Absent for a missing-but-desired
// target: Capture leaves the previously desired state untouched.
func TestInspectCapturePreservesMissingTargetWhenProviderPreservesIt(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "resources", targets: []TargetInspection{
		{
			Key: "resource:dotfiles", CaptureEligible: true, Current: TargetAbsent, Desired: TargetPresent,
			Capabilities: TargetCapabilities{SupportsDesiredAbsence: false, PreservesMissingDesired: true},
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

// A provider that cannot express desired absence AND does not preserve a
// missing target (e.g. Defaults/Shell: Capture always writes a fresh full
// replacement, so a vanished value is silently dropped, not remembered)
// reports StopManaging, distinct from Absent: Blueprint carries no desired
// state for the target afterward, rather than remembering its removal.
// SupportsDesiredAbsence alone must not decide this.
func TestInspectCaptureReportsStopManagingWhenProviderStopsManagingMissingTarget(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "defaults", targets: []TargetInspection{
		{
			Key: "terminal", CaptureEligible: true, Current: TargetAbsent, Desired: TargetPresent,
			Capabilities: TargetCapabilities{SupportsDesiredAbsence: false, PreservesMissingDesired: false},
		},
	}}})

	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := singleCaptureTarget(t, inspection, "defaults")
	if got.Outcome != CaptureOutcomeStopManaging {
		t.Fatalf("Outcome = %s, want %s", got.Outcome, CaptureOutcomeStopManaging)
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

// TestInspectCaptureReportsStopManagingForOwnershipTransitionInsteadOfBlocked
// is a regression for a review finding on PR 3: an ineligible target whose
// provider marks it DropsDesiredWhenIneligible (an intentional ownership-
// management transition, e.g. Config Delegated/Excluded) actually loses its
// desired state when Capture runs, unlike a generic safety freeze. Reporting
// it as Blocked (implying nothing changes) would be misleading, so it must
// report StopManaging instead when it was desired-present.
func TestInspectCaptureReportsStopManagingForOwnershipTransitionInsteadOfBlocked(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "config", targets: []TargetInspection{
		{
			Key: ".config/nvim/init.lua", CaptureEligible: false, Current: TargetPresent, Desired: TargetPresent,
			SafetyReason: "delegated to another provider",
			Capabilities: TargetCapabilities{DropsDesiredWhenIneligible: true},
		},
	}}})

	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := singleCaptureTarget(t, inspection, "config")
	if got.Outcome != CaptureOutcomeStopManaging {
		t.Fatalf("Outcome = %s, want %s", got.Outcome, CaptureOutcomeStopManaging)
	}
}

// TestInspectCaptureReportsStopManagingForOwnershipTransitionOfADesiredAbsentTombstone
// is a regression for a round-3 review finding on PR 3: captureOutcome
// only checked target.Desired == TargetPresent, so an ownership-transition
// target holding a desired-absent tombstone (e.g. a saved Config deletion
// whose path later becomes Excluded/Delegated) still previewed as generic
// Blocked, even though real Capture drops the tombstone exactly the same
// way it drops desired-present state for these classifications (see
// capture.go's ConfigDelegated/ConfigExcluded branch, which does not
// distinguish a saved file from a saved deletion). Any recorded desired
// state, not just desired-present, must report StopManaging here.
func TestInspectCaptureReportsStopManagingForOwnershipTransitionOfADesiredAbsentTombstone(t *testing.T) {
	got := captureOutcome(TargetInspection{
		CaptureEligible: false, Current: TargetAbsent, Desired: TargetAbsent,
		Capabilities: TargetCapabilities{DropsDesiredWhenIneligible: true},
	}, CaptureDecision{Capture: false, Resolved: true})
	if got != CaptureOutcomeStopManaging {
		t.Fatalf("Outcome = %s, want %s", got, CaptureOutcomeStopManaging)
	}
}

// TestInspectCaptureOwnershipTransitionWithNoPriorDesiredStateStaysBlocked
// confirms the distinction only applies when there was something to drop:
// a target that was never desired-present has nothing for Capture to
// actively remove, so it stays a plain Blocked report.
func TestInspectCaptureOwnershipTransitionWithNoPriorDesiredStateStaysBlocked(t *testing.T) {
	got := captureOutcome(TargetInspection{
		CaptureEligible: false, Current: TargetPresent, Desired: TargetUnknown,
		Capabilities: TargetCapabilities{DropsDesiredWhenIneligible: true},
	}, CaptureDecision{Capture: false, Resolved: true})
	if got != CaptureOutcomeBlocked {
		t.Fatalf("Outcome = %s, want %s", got, CaptureOutcomeBlocked)
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
