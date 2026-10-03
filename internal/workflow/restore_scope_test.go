package workflow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func deferralSession(t *testing.T) (*Session, *RestoreContext, *RestoreContext) {
	t.Helper()
	session := newCaptureSession(t, profile.New("test", time.Now()))
	packages, services := &RestoreContext{}, &RestoreContext{}
	session.SetProviders([]Provider{
		captureTestProvider{id: "packages", order: &[]string{}, lastPlan: packages, targets: []TargetInspection{{Key: "official:firefox"}}},
		captureTestProvider{id: "services", order: &[]string{}, lastPlan: services, targets: []TargetInspection{{Key: "waybar.service"}}},
		captureTestProvider{id: "themes", order: &[]string{}, targets: []TargetInspection{{Key: "theme:tokyo-night"}}},
	})
	return session, packages, services
}

func TestDeferredCategoriesAreLeftOutOfThePlanAndRecorded(t *testing.T) {
	session, packages, services := deferralSession(t)
	plan, providers, _, _, err := session.PlanRestoreScopeWithContext(context.Background(), RestoreScope{Defer: []string{"services", "packages", "services"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Deferred, []string{"packages", "services"}) {
		t.Fatalf("Deferred = %v, want sorted and de-duplicated packages, services", plan.Deferred)
	}
	if packages.Targets != nil || services.Targets != nil {
		t.Fatal("a deferred category was planned")
	}
	if len(providers) != 1 || providers[0].ID() != "themes" {
		t.Fatalf("selected providers = %v, want only themes", providers)
	}
	for _, category := range plan.Compatibility.Categories {
		if category.Category != "themes" {
			t.Fatalf("deferred category %s contributed compatibility", category.Category)
		}
	}
}

func TestDeferredPreviewIsTheAuthoritativePlan(t *testing.T) {
	session, _, _ := deferralSession(t)
	scope := RestoreScope{Defer: []string{"services"}}
	preview, err := session.PreviewRestoreScope(context.Background(), scope, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, _, _, _, err := session.PlanRestoreScopeWithContext(context.Background(), scope, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Plan, plan) {
		t.Fatalf("preview and authoritative plan differ:\n%#v\n%#v", preview.Plan, plan)
	}
}

func TestApprovalCoversTheDeferralItWasGivenFor(t *testing.T) {
	session, _, _ := deferralSession(t)
	approved, err := session.PlanRestore(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.ApplyApprovedRestoreScope(context.Background(), RestoreScope{Defer: []string{"services"}}, nil, approved, false)
	if !errors.Is(err, ErrRestorePlanChanged) {
		t.Fatalf("applying a different deferral than the approved plan: err = %v, want ErrRestorePlanChanged", err)
	}
}

func TestDeferralRejectsUnknownEverythingAndSingleCategoryScopes(t *testing.T) {
	session, _, _ := deferralSession(t)
	for _, tc := range []struct {
		scope RestoreScope
		want  string
	}{
		{RestoreScope{Defer: []string{"bogus"}}, "unknown category"},
		{RestoreScope{Defer: []string{"packages", "services", "themes"}}, "nothing is left"},
		{RestoreScope{Only: "themes", Defer: []string{"services"}}, "cannot be combined"},
	} {
		_, _, _, _, err := session.PlanRestoreScopeWithContext(context.Background(), tc.scope, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("scope %+v: err = %v, want %q", tc.scope, err, tc.want)
		}
		if _, err := session.PreviewRestoreScope(context.Background(), tc.scope, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("preview scope %+v: err = %v, want %q", tc.scope, err, tc.want)
		}
	}
}
