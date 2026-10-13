package workflow

import (
	"context"
	"errors"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type failingArtifactCapture struct {
	broadScopedCaptureProvider
	root string
}

func (p failingArtifactCapture) Capture(context.Context, *profile.Data, CaptureContext) (any, []model.Change, error) {
	if err := os.MkdirAll(filepath.Join(p.root, "hooks"), 0700); err != nil {
		return nil, nil, err
	}
	os.WriteFile(filepath.Join(p.root, "hooks/partial.sh"), []byte("partial artifact"), 0600)
	return nil, nil, errors.New("injected staged artifact failure")
}
func TestCaptureTargetScopeFailureRestoresArtifactsEvenWithoutProviderTransaction(t *testing.T) {
	s := newCaptureSession(t, profile.New("test", time.Now()))
	order := []string{}
	p := failingArtifactCapture{root: s.ProfileDir(), broadScopedCaptureProvider: broadScopedCaptureProvider{captureTestProvider{id: "packages", order: &order, targets: []TargetInspection{{Key: "official:a", Current: TargetPresent, CaptureEligible: true}}}}}
	s.SetProviders([]Provider{p})
	scope := TargetSelection{"packages": {"official:a"}}
	review, err := s.InspectCaptureTargetsFresh(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaptureApprovedTargets(context.Background(), scope, review); err == nil {
		t.Fatal("failed Capture reported success")
	}
	if _, err := os.Stat(filepath.Join(s.ProfileDir(), "hooks/partial.sh")); !os.IsNotExist(err) {
		t.Fatal("failed Capture left staged profile artifacts")
	}
}

type broadScopedCaptureProvider struct{ captureTestProvider }

type crossCategoryCaptureProvider struct{ captureTestProvider }

func (crossCategoryCaptureProvider) SyncDesired(profile.Data) ([]syncengine.DesiredTarget, error) {
	return nil, nil
}

func TestCaptureTargetScopeRejectsCrossCategoryExpansion(t *testing.T) {
	d := profile.New("test", time.Now())
	d.Packages.Official = []string{"a"}
	s := newCaptureSession(t, d)
	order := []string{}
	s.SetProviders([]Provider{
		crossCategoryCaptureProvider{captureTestProvider{id: "hooks", order: &order, targets: []TargetInspection{{Key: "a", Current: TargetPresent, CaptureEligible: true}}}},
		broadScopedCaptureProvider{captureTestProvider{id: "packages", order: &order}},
	})
	scope := TargetSelection{"hooks": {"a"}}
	review, err := s.InspectCaptureTargetsFresh(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaptureApprovedTargets(context.Background(), scope, review); err == nil {
		t.Fatal("selected provider changed unselected category")
	}
	loaded, err := profile.Load(s.ProfileDir())
	if err != nil || len(loaded.Packages.Official) != 1 {
		t.Fatalf("cross-category expansion was saved: %v", err)
	}
}

func (b broadScopedCaptureProvider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	return (packages.Provider{}).SyncDesired(d)
}
func TestCaptureTargetScopeRejectsProviderExpansionBeforeSave(t *testing.T) {
	d := profile.New("test", time.Now())
	d.Packages.Official = []string{"a"}
	s := newCaptureSession(t, d)
	order := []string{}
	s.SetProviders([]Provider{broadScopedCaptureProvider{captureTestProvider{id: "packages", order: &order, targets: []TargetInspection{{Key: "official:a", Current: TargetPresent, CaptureEligible: true}}}}})
	scope := TargetSelection{"packages": {"official:a"}}
	review, err := s.InspectCaptureTargetsFresh(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaptureApprovedTargets(context.Background(), scope, review); err == nil {
		t.Fatal("unselected desired state was saved")
	}
	loaded, err := profile.Load(s.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Packages.Official) != 1 || loaded.Packages.Official[0] != "a" {
		t.Fatal("scope expansion reached profile")
	}
}

func TestCaptureTargetScopeNeverEnablesUnselectedTargets(t *testing.T) {
	s := newCaptureSession(t, profile.New("test", time.Now()))
	order := []string{}
	captured := CaptureContext{}
	p := captureTestProvider{id: "packages", order: &order, lastCapture: &captured, targets: []TargetInspection{
		{Key: "official:a", Current: TargetPresent, CaptureEligible: true},
		{Key: "official:b", Current: TargetPresent, CaptureEligible: true},
	}}
	s.SetProviders([]Provider{p})
	scope := TargetSelection{"packages": {"official:a"}}
	review, err := s.InspectCaptureTargetsFresh(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaptureApprovedTargets(context.Background(), scope, review); err != nil {
		t.Fatal(err)
	}
	if !captured.Targets["official:a"].Capture || captured.Targets["official:b"].Capture {
		t.Fatalf("scope expanded: %#v", captured)
	}
}

func TestRestoreTargetScopePreservesSafetyAndDoesNotBroaden(t *testing.T) {
	last := RestoreContext{}
	s, _, _ := approvedRestoreSession(t, captureTestProvider{id: "packages", lastPlan: &last, targets: []TargetInspection{
		{Key: "official:a", Current: TargetPresent, Desired: TargetPresent, RestoreEligible: true},
		{Key: "official:b", Current: TargetPresent, Desired: TargetPresent, RestoreEligible: true},
	}})
	scope := RestoreScope{Targets: TargetSelection{"packages": {"official:a"}}}
	if _, _, _, _, err := s.PlanRestoreScopeWithContext(context.Background(), scope, nil); err != nil {
		t.Fatal(err)
	}
	if !last.Targets["official:a"].Restore || last.Targets["official:b"].Restore || last.Targets["official:b"].CompatibilityApply {
		t.Fatalf("scope expanded: %#v", last)
	}
}

func TestRestoreTargetScopeRejectsUnattributedOrExpandedOperations(t *testing.T) {
	s, _, _ := approvedRestoreSession(t, captureTestProvider{id: "packages", interactive: true, targets: []TargetInspection{{Key: "official:a", Current: TargetPresent, Desired: TargetPresent, RestoreEligible: true}}})
	_, _, _, _, err := s.PlanRestoreScopeWithContext(context.Background(), RestoreScope{Targets: TargetSelection{"packages": {"official:a"}}}, nil)
	if err == nil {
		t.Fatal("unattributed operation escaped target scope")
	}
}
