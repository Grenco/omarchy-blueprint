package workflow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func testCompatibility(state model.CompatibilityState, authority model.CompatibilityAuthority) model.CompatibilityReport {
	return model.CompatibilityReport{Target: model.CompatibilityEnvironment{Known: true, OmarchyVersion: "4.0.0"}, Categories: []model.CompatibilityCategory{{Category: "packages", Applies: true, State: state, Authority: authority, Findings: []model.CompatibilityFinding{{Code: "packages.test", Target: "official:git", State: state, Authority: authority, Summary: "stable test finding"}}}}}
}

func TestPlanRestoreBuildsCompatibilityEnvironmentContext(t *testing.T) {
	data := profile.New("test", time.Unix(1, 0))
	data.Manifest.Omarchy.CapturedVersion = "3.0.0"
	data.Manifest.Omarchy.Channel = "stable"
	s := newCaptureSession(t, data)
	s.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}, targets: []TargetInspection{{Key: "official:git", RestoreEligible: true}}}})
	plan, err := s.PlanRestore(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Compatibility.ProfileLastCapture.Known || plan.Compatibility.ProfileLastCapture.OmarchyVersion != "3.0.0" || plan.Compatibility.Target.OmarchyVersion != "4.0.0" || plan.Compatibility.Target.OmarchyChannel != "stable" || len(plan.Compatibility.Categories) != 1 {
		t.Fatalf("environment/report = %+v", plan.Compatibility)
	}
}

func TestPlanRestoreUnknownProfileLastCaptureRemainsUsable(t *testing.T) {
	s := newCaptureSession(t, profile.New("test", time.Unix(1, 0)))
	s.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}}})
	plan, err := s.PlanRestore(context.Background(), "", nil)
	if err != nil || plan.Compatibility.ProfileLastCapture.Known {
		t.Fatalf("unknown historical context rejected or guessed: %+v err=%v", plan.Compatibility, err)
	}
}

func TestPlanRestoreRequiresOneCompatibilityCategoryPerSelectedProvider(t *testing.T) {
	s := newCaptureSession(t, profile.New("test", time.Unix(1, 0)))
	s.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}, noCompatibility: true}})
	if _, err := s.PlanRestore(context.Background(), "", nil); err == nil {
		t.Fatal("selected provider omitted compatibility assessment")
	}
}

func TestPlanRestoreAllSkippedProviderIsNotApplicable(t *testing.T) {
	data := profile.New("test", time.Unix(1, 0))
	data.Policy = policy.Rules{Restore: []policy.Rule{{Category: "packages", Target: "official:git", Setting: policy.SettingDisabled}}}
	s := newCaptureSession(t, data)
	s.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}, targets: []TargetInspection{{Key: "official:git", RestoreEligible: true}}}})
	plan, err := s.PlanRestore(context.Background(), "", nil)
	if err != nil || len(plan.Compatibility.Categories) != 1 || plan.Compatibility.Categories[0].Applies || plan.Compatibility.Categories[0].State != "" {
		t.Fatalf("all skipped = %+v err=%v", plan.Compatibility, err)
	}
}

func TestCheckRestoreApplicableRejectsBlockedCompatibilityBeforeRequirements(t *testing.T) {
	report := testCompatibility(model.CompatibilityUnknown, model.CompatibilityBlocked)
	report.Categories[0].Findings[0].RequirementID = "packages.metadata"
	plan := model.RestorePlan{Compatibility: report, Requirements: []model.Requirement{{ID: "packages.metadata", Reason: "metadata unavailable", Remediation: []string{"omarchy", "update"}}, {ID: "other", Reason: "unrelated"}}}
	var blocked *BlockedCompatibilityError
	if err := CheckRestoreApplicable(plan, false); !errors.As(err, &blocked) || len(blocked.Findings) != 1 || blocked.Findings[0].Code != "packages.test" || len(blocked.LinkedRequirements) != 1 || blocked.LinkedRequirements[0].ID != "packages.metadata" || !strings.Contains(err.Error(), "omarchy update") || strings.Contains(err.Error(), "unrelated") {
		t.Fatalf("compatibility was not first/structured: %v", err)
	}
}

func TestCheckRestoreApplicableForceCannotBypassCompatibility(t *testing.T) {
	plan := model.RestorePlan{Compatibility: testCompatibility(model.CompatibilityIncompatible, model.CompatibilityBlocked)}
	for _, terminal := range []bool{false, true} {
		var blocked *BlockedCompatibilityError
		if err := CheckRestoreApplicable(plan, terminal); !errors.As(err, &blocked) {
			t.Fatalf("terminal/Force-like authority bypassed compatibility: %v", err)
		}
	}
}

func TestScopedRestoreIgnoresUnselectedIncompatibleCategory(t *testing.T) {
	plan := model.RestorePlan{Compatibility: model.CompatibilityReport{Categories: []model.CompatibilityCategory{{Category: "resources", Applies: true, State: model.CompatibilitySupported, Authority: model.CompatibilityUnchanged, Evidence: []model.CompatibilityEvidence{{Kind: "portable", Summary: "safe resource"}}}}}}
	if err := CheckRestoreApplicable(plan, false); err != nil {
		t.Fatalf("scoped supported plan rejected: %v", err)
	}
}

func TestApplyApprovedRestoreInvalidatesBothCompatibilityDirections(t *testing.T) {
	for _, tc := range []struct{ before, after model.CompatibilityState }{
		{model.CompatibilitySupported, model.CompatibilityUnknown},
		{model.CompatibilityUnknown, model.CompatibilitySupported},
	} {
		s := newCaptureSession(t, profile.New("test", time.Unix(1, 0)))
		state := tc.before
		s.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}, compatibilityState: &state, targets: []TargetInspection{{Key: "official:git", RestoreEligible: true}}}})
		approved, err := s.PlanRestore(context.Background(), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		state = tc.after
		result, err := s.ApplyApprovedRestore(context.Background(), "", nil, approved, false)
		if !errors.Is(err, ErrRestorePlanChanged) || result.Applied || reflect.DeepEqual(result.Plan, approved) {
			t.Fatalf("%s -> %s: approval retained authority: result=%+v err=%v", tc.before, tc.after, result, err)
		}
	}
}

func TestVerifyReceivesNormalizedPlannedCompatibilityCategory(t *testing.T) {
	s := newCaptureSession(t, profile.New("test", time.Unix(1, 0)))
	var verified RestoreContext
	s.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}, lastVerify: &verified,
		targets:               []TargetInspection{{Key: "official:git", RestoreEligible: true}},
		compatibilityEvidence: []model.CompatibilityEvidence{{Kind: "z", Summary: "last"}, {Kind: "a", Summary: "first"}},
	}})
	result, err := s.ApplyRestore(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(verified.Compatibility, result.Plan.Compatibility.Categories[0]) || verified.Compatibility.Evidence[0].Kind != "a" {
		t.Fatalf("Verify category %+v differs from authoritative plan %+v", verified.Compatibility, result.Plan.Compatibility.Categories[0])
	}
}
