package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type servicesVersionRunner struct{}

func (servicesVersionRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if name == "omarchy" && len(args) > 0 && args[0] == "version" {
		if len(args) > 1 && args[1] == "channel" {
			return "stable", nil
		}
		return "4.0.0", nil
	}
	return "", errors.New("unexpected command in Services Capture")
}

func servicesWorkflowSession(t *testing.T, provider *Provider) *workflow.Session {
	t.Helper()
	state := t.TempDir()
	s, err := workflow.Open(workflow.Dependencies{Runner: servicesVersionRunner{}, Now: func() time.Time { return time.Unix(2, 0) }, StateHome: func() (string, error) { return state, nil }}, workflow.Options{ProfileDir: provider.ProfileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProviders([]workflow.Provider{provider}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReviewedCaptureCommitsServiceThroughWorkflow(t *testing.T) {
	p, _, dir, _ := captureFixture(t)
	s := servicesWorkflowSession(t, p)
	inspection, err := s.InspectCaptureMany(context.Background(), []string{"services"})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Categories["services"][0].Outcome != workflow.CaptureOutcomeUnselected {
		t.Fatalf("new unit was preselected: %+v", inspection)
	}
	approved, err := inspection.SelectCandidate("services", "backup.service", true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.CaptureApproved(context.Background(), []string{"services"}, approved)
	if err != nil || len(result.Providers) != 1 || result.Providers[0] != "services" {
		t.Fatalf("reviewed Capture failed: result=%+v err=%v", result, err)
	}
	loaded, err := profile.Load(dir)
	if err != nil || !loaded.Manifest.Capture.Services || len(loaded.Services.Units) != 1 || p.prepared != nil {
		t.Fatalf("service profile was not committed atomically: state=%+v err=%v", loaded.Services, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "services", "units", "backup.service")); err != nil {
		t.Fatal(err)
	}
}

func TestReviewedCaptureRejectsChangedServiceCandidateAfterApproval(t *testing.T) {
	p, _, dir, live := captureFixture(t)
	s := servicesWorkflowSession(t, p)
	inspection, err := s.InspectCaptureMany(context.Background(), []string{"services"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := inspection.SelectCandidate("services", "backup.service", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.CaptureApproved(context.Background(), []string{"services"}, approved)
	var changed *workflow.CaptureReviewChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("changed service retained approval: %v", err)
	}
	loaded, err := profile.Load(dir)
	if err != nil || loaded.Manifest.Capture.Services || len(loaded.Services.Units) != 0 {
		t.Fatalf("failed approval changed profile: state=%+v err=%v", loaded.Services, err)
	}
}
