package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func selectionTargets() []TargetInspection {
	return []TargetInspection{
		{Key: "backup.timer", Current: TargetPresent, Desired: TargetUnknown, CaptureEligible: true, RequiresSelection: true, RecommendedDependencies: []string{"backup.service"}, Fingerprint: "timer-a"},
		{Key: "backup.service", Current: TargetPresent, Desired: TargetUnknown, CaptureEligible: true, RequiresSelection: true, Fingerprint: "service-a"},
		{Key: "external.service", Current: TargetPresent, Desired: TargetUnknown, CaptureEligible: false, RequiresSelection: true, SafetyReason: "External service"},
	}
}

func TestCaptureReviewSelectionIsExplicitAndDependenciesCanBeDeclined(t *testing.T) {
	s := newCaptureSession(t, profile.New("test", time.Unix(1, 0)))
	var order []string
	var captured CaptureContext
	s.SetProviders([]Provider{captureTestProvider{id: "services", order: &order, targets: selectionTargets(), lastCapture: &captured}})
	inspection, err := s.InspectCaptureMany(context.Background(), []string{"services"})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range inspection.Categories["services"][:2] {
		if target.Selected || target.Outcome != CaptureOutcomeUnselected {
			t.Fatalf("new candidate became owned without review: %+v", target)
		}
	}
	if _, err := s.CaptureMany(context.Background(), []string{"services"}); err != nil || captured.Targets["backup.timer"].Selected {
		t.Fatalf("plain Capture selected new service: %+v err=%v", captured, err)
	}
	selected, err := inspection.SelectCandidate("services", "backup.timer", true)
	if err != nil {
		t.Fatal(err)
	}
	if !selected.Categories["services"][0].Selected || !selected.Categories["services"][1].Selected || selected.Categories["services"][0].Outcome != CaptureOutcomeAdd {
		t.Fatalf("selected parent did not recommend the child: %+v", selected)
	}
	selected, err = selected.SelectCandidate("services", "backup.service", false)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Categories["services"][1].Selected || selected.Categories["services"][1].Outcome != CaptureOutcomeUnselected {
		t.Fatalf("dependency could not be deselected: %+v", selected)
	}
	if _, err := selected.SelectCandidate("services", "external.service", true); err == nil {
		t.Fatal("external ineligible service was selected")
	}
	if _, err := s.CaptureApproved(context.Background(), []string{"services"}, selected); err != nil || !captured.Targets["backup.timer"].Selected || captured.Targets["backup.service"].Selected {
		t.Fatalf("approval lost selected/deselected authority: %+v err=%v", captured, err)
	}
}

func TestCaptureApprovalRejectsChangedSelectedServiceFingerprint(t *testing.T) {
	s := newCaptureSession(t, profile.New("test", time.Unix(1, 0)))
	var order []string
	targets := selectionTargets()
	s.SetProviders([]Provider{captureTestProvider{id: "services", order: &order, targets: targets}})
	inspection, err := s.InspectCaptureMany(context.Background(), []string{"services"})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := inspection.SelectCandidate("services", "backup.timer", true)
	if err != nil {
		t.Fatal(err)
	}
	targets[0].Fingerprint = "timer-b"
	_, err = s.CaptureApproved(context.Background(), []string{"services"}, selected)
	var changed *CaptureReviewChangedError
	if !errors.As(err, &changed) || len(order) != 0 {
		t.Fatalf("changed selected candidate was captured: err=%v capture=%v", err, order)
	}
}

func TestCaptureApprovalRejectsChangedServiceRecommendations(t *testing.T) {
	s := newCaptureSession(t, profile.New("test", time.Unix(1, 0)))
	var order []string
	targets := selectionTargets()
	s.SetProviders([]Provider{captureTestProvider{id: "services", order: &order, targets: targets}})
	inspection, err := s.InspectCaptureMany(context.Background(), []string{"services"})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := inspection.SelectCandidate("services", "backup.timer", true)
	if err != nil {
		t.Fatal(err)
	}
	targets[0].RecommendedDependencies = nil
	_, err = s.CaptureApproved(context.Background(), []string{"services"}, selected)
	var changed *CaptureReviewChangedError
	if !errors.As(err, &changed) || len(order) != 0 {
		t.Fatalf("changed dependency recommendation retained approval: err=%v capture=%v", err, order)
	}
}
