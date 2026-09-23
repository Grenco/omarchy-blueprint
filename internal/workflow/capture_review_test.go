package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// captureReviewTestProvider reports factual differences through Diff and
// attributes them to target keys, so InspectCapture can tell an unchanged
// tracked target from one Capture would really rewrite.
type captureReviewTestProvider struct {
	captureInspectionTestProvider
	changes []model.Change
	diffErr error
}

func (p captureReviewTestProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return p.changes, p.diffErr
}

func (captureReviewTestProvider) ChangeTargetKey(change model.Change) (string, bool) {
	switch {
	case change.Kind == "settings":
		return "", true
	case change.Kind == "" || change.Name == "":
		return "", false
	}
	return change.Kind + ":" + change.Name, true
}

func TestCaptureOutcomeAndReviewGroupTable(t *testing.T) {
	enabled, disabled := CaptureDecision{Capture: true, Resolved: true}, CaptureDecision{Capture: false, Resolved: true}
	for _, test := range []struct {
		name     string
		target   TargetInspection
		decision CaptureDecision
		differs  bool
		outcome  CaptureOutcome
		group    CaptureReviewGroup
	}{
		{"new target is added", TargetInspection{CaptureEligible: true, Current: TargetPresent, Desired: TargetUnknown}, enabled, true, CaptureOutcomeAdd, CaptureReviewChanges},
		{"changed tracked target is updated", TargetInspection{CaptureEligible: true, Current: TargetPresent, Desired: TargetPresent}, enabled, true, CaptureOutcomeUpdate, CaptureReviewChanges},
		{"unchanged tracked target needs nothing", TargetInspection{CaptureEligible: true, Current: TargetPresent, Desired: TargetPresent}, enabled, false, CaptureOutcomeNoop, CaptureReviewNoAction},
		{"removed target is remembered absent", TargetInspection{CaptureEligible: true, Current: TargetAbsent, Desired: TargetPresent, Capabilities: TargetCapabilities{SupportsDesiredAbsence: true}}, enabled, true, CaptureOutcomeAbsent, CaptureReviewChanges},
		{"removed target without absence stops managing", TargetInspection{CaptureEligible: true, Current: TargetAbsent, Desired: TargetPresent}, enabled, true, CaptureOutcomeStopManaging, CaptureReviewChanges},
		{"policy preserves a removed target", TargetInspection{CaptureEligible: true, Current: TargetAbsent, Desired: TargetPresent, Capabilities: TargetCapabilities{SupportsDesiredAbsence: true}}, disabled, true, CaptureOutcomePreserve, CaptureReviewPreserved},
		{"policy keeps a new target unmanaged", TargetInspection{CaptureEligible: true, Current: TargetPresent, Desired: TargetUnknown}, disabled, true, CaptureOutcomePreserve, CaptureReviewPreserved},
		{"provider keeps a missing target", TargetInspection{CaptureEligible: true, Current: TargetAbsent, Desired: TargetPresent, Capabilities: TargetCapabilities{PreservesMissingDesired: true}}, enabled, true, CaptureOutcomePreserve, CaptureReviewNoAction},
		{"safety blocks regardless of policy", TargetInspection{CaptureEligible: false, Current: TargetPresent, Desired: TargetUnknown, SafetyReason: "sensitive"}, enabled, true, CaptureOutcomeBlocked, CaptureReviewBlocked},
		{"nothing on either side", TargetInspection{CaptureEligible: true, Current: TargetAbsent, Desired: TargetUnknown}, enabled, false, CaptureOutcomeNoop, CaptureReviewNoAction},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := CaptureTarget{Inspection: test.target, Decision: test.decision, Outcome: captureOutcomeFor(test.target, test.decision, test.differs)}
			if target.Outcome != test.outcome {
				t.Fatalf("outcome = %s, want %s", target.Outcome, test.outcome)
			}
			if got := target.ReviewGroup(); got != test.group {
				t.Fatalf("group = %s, want %s", got, test.group)
			}
		})
	}
}

func TestInspectCaptureUsesAttributedDifferencesForTrackedTargets(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureReviewTestProvider{
		captureInspectionTestProvider: captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
			{Key: "official:firefox", CaptureEligible: true, Current: TargetPresent, Desired: TargetPresent},
			{Key: "mise:node", CaptureEligible: true, Current: TargetPresent, Desired: TargetPresent},
		}},
		changes: []model.Change{{Type: model.ChangeModify, Provider: "packages", Kind: "mise", Name: "node"}},
	}})
	inspection, err := session.InspectCapture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	outcomes := map[string]CaptureOutcome{}
	for _, target := range inspection.Categories["packages"] {
		outcomes[target.Inspection.Key] = target.Outcome
	}
	if outcomes["mise:node"] != CaptureOutcomeUpdate || outcomes["official:firefox"] != CaptureOutcomeNoop {
		t.Fatalf("outcomes = %#v, want changed node Update and unchanged firefox Noop", outcomes)
	}
}

func TestInspectCaptureTreatsUnattributedChangesConservatively(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  model.Change
		outcome CaptureOutcome
	}{
		{"change with no target leaves unchanged targets alone", model.Change{Kind: "settings", Name: "exclusions"}, CaptureOutcomeNoop},
		{"unattributable change makes every tracked target an update", model.Change{Summary: "something changed"}, CaptureOutcomeUpdate},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := newInspectionSession(t)
			session.SetProviders([]Provider{captureReviewTestProvider{
				captureInspectionTestProvider: captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
					{Key: "official:firefox", CaptureEligible: true, Current: TargetPresent, Desired: TargetPresent},
				}},
				changes: []model.Change{test.change},
			}})
			inspection, err := session.InspectCapture(context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			if got := singleCaptureTarget(t, inspection, "packages").Outcome; got != test.outcome {
				t.Fatalf("outcome = %s, want %s", got, test.outcome)
			}
		})
	}
}

func TestInspectCaptureReportsDiffErrors(t *testing.T) {
	session := newInspectionSession(t)
	session.SetProviders([]Provider{captureReviewTestProvider{
		captureInspectionTestProvider: captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
			{Key: "official:firefox", CaptureEligible: true, Current: TargetPresent, Desired: TargetPresent},
		}},
		diffErr: errors.New("pacman unavailable"),
	}})
	if _, err := session.InspectCapture(context.Background(), ""); err == nil {
		t.Fatal("a failed Diff must fail inspection rather than guess whether targets changed")
	}
}

func TestCaptureReviewGroupsAreOrderedAndDeterministic(t *testing.T) {
	session := newPolicyReviewSession(t)
	session.SetProviders([]Provider{
		captureInspectionTestProvider{id: "themes", targets: []TargetInspection{
			{Key: "theme:nord", Label: "nord", CaptureEligible: true, Current: TargetPresent, Desired: TargetUnknown},
			{Key: "theme:bad", Label: "bad", CaptureEligible: false, Current: TargetPresent, Desired: TargetUnknown, SafetyReason: "unsafe source"},
		}},
		captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
			{Key: "official:zsh", Label: "zsh", CaptureEligible: true, Current: TargetPresent, Desired: TargetUnknown},
			{Key: "official:bash", Label: "bash", CaptureEligible: true, Current: TargetPresent, Desired: TargetUnknown},
			{Key: "official:gone", Label: "gone", CaptureEligible: true, Current: TargetAbsent, Desired: TargetUnknown},
		}},
	})
	if err := session.SetPolicy(PolicyScope{}, policy.AxisCapture, "packages", "official:zsh", policy.SettingDisabled); err != nil {
		t.Fatal(err)
	}
	var first []CaptureReviewSection
	for attempt := 0; attempt < 5; attempt++ {
		inspection, err := session.InspectCapture(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		review := inspection.Review()
		if attempt == 0 {
			first = review
			continue
		}
		if !reflect.DeepEqual(review, first) {
			t.Fatalf("review order changed between inspections:\n%#v\n%#v", first, review)
		}
	}
	got := [][]string{}
	for _, section := range first {
		keys := []string{string(section.Group)}
		for _, target := range section.Targets {
			keys = append(keys, target.Category+"/"+target.Inspection.Key)
		}
		got = append(got, keys)
	}
	want := [][]string{
		{string(CaptureReviewChanges), "packages/official:bash", "themes/theme:nord"},
		{string(CaptureReviewPreserved), "packages/official:zsh"},
		{string(CaptureReviewBlocked), "themes/theme:bad"},
		{string(CaptureReviewNoAction), "packages/official:gone"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("review =\n%v\nwant\n%v", got, want)
	}
}

func TestCaptureReviewOmitsEmptyGroups(t *testing.T) {
	inspection := CaptureInspection{Categories: map[string][]CaptureTarget{
		"packages": {{Category: "packages", Inspection: TargetInspection{Key: "official:bash"}, Outcome: CaptureOutcomeAdd, Decision: CaptureDecision{Capture: true, Resolved: true}}},
	}}
	review := inspection.Review()
	if len(review) != 1 || review[0].Group != CaptureReviewChanges {
		t.Fatalf("review = %#v, want only Changes", review)
	}
}

func TestInspectCaptureRecalculatesAfterPolicyMutation(t *testing.T) {
	session := newPolicyReviewSession(t)
	session.SetProviders([]Provider{captureInspectionTestProvider{id: "packages", targets: []TargetInspection{
		{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: TargetPresent, Desired: TargetUnknown},
	}}})
	before, err := session.InspectCapture(context.Background(), "packages")
	if err != nil {
		t.Fatal(err)
	}
	if got := singleCaptureTarget(t, before, "packages"); got.Outcome != CaptureOutcomeAdd || got.ReviewGroup() != CaptureReviewChanges {
		t.Fatalf("before policy change: %s/%s, want add in Changes", got.Outcome, got.ReviewGroup())
	}
	if err := session.SetPolicy(PolicyScope{}, policy.AxisCapture, "packages", "official:firefox", policy.SettingDisabled); err != nil {
		t.Fatal(err)
	}
	after, err := session.InspectCapture(context.Background(), "packages")
	if err != nil {
		t.Fatal(err)
	}
	if got := singleCaptureTarget(t, after, "packages"); got.Outcome != CaptureOutcomePreserve || got.ReviewGroup() != CaptureReviewPreserved {
		t.Fatalf("after Ignore: %s/%s, want preserve in Preserved by policy", got.Outcome, got.ReviewGroup())
	}
	if err := session.ClearPolicy(PolicyScope{}, policy.AxisCapture, "packages", "official:firefox"); err != nil {
		t.Fatal(err)
	}
	reset, err := session.InspectCapture(context.Background(), "packages")
	if err != nil {
		t.Fatal(err)
	}
	if got := singleCaptureTarget(t, reset, "packages"); got.Outcome != CaptureOutcomeAdd {
		t.Fatalf("after reset: %s, want add again", got.Outcome)
	}
}

// newPolicyReviewSession can persist policy mutations, which need a clock.
func newPolicyReviewSession(t *testing.T) *Session {
	t.Helper()
	profileDir, stateHome := t.TempDir(), t.TempDir()
	if err := profile.Save(profileDir, profile.New("test", time.Now())); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{Now: func() time.Time { return time.Unix(1, 0) }, StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	return session
}
