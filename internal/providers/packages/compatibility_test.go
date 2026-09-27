package packages

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestRestoreCompatibilityOriginUnknownIsNotSupported(t *testing.T) {
	saved := profile.Packages{Official: []string{"git"}}
	current := freshCurrent([]string{"git"}, "git")
	got, err := RestoreCompatibility(saved, current, map[string]bool{"official:git": true}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.CompatibilityUnknown || got.Authority != model.CompatibilityUnchanged || !got.Applies || len(got.Findings) != 1 || got.Findings[0].Code != "packages.origin.unknown" || got.Findings[0].Target != "official:git" || got.Findings[0].RequirementID != "" {
		t.Fatalf("installed package with unknown origin = %+v", got)
	}
}

func TestRestoreCompatibilityOriginUnknownExactIsReduced(t *testing.T) {
	saved := profile.Packages{Absent: []profile.PackageAbsence{{Ref: "official:htop"}, {Ref: "aur:yay"}}}
	current := freshCurrent([]string{"htop", "yay"}, "htop", "yay")
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.0", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := RestoreCompatibility(saved, current, map[string]bool{"official:htop": true, "aur:yay": true}, true, plan.Requirements)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.CompatibilityUnknown || got.Authority != model.CompatibilityReduced || len(got.Findings) != 2 {
		t.Fatalf("unknown-origin Exact = %+v", got)
	}
	for _, finding := range got.Findings {
		if finding.Code != "packages.origin.unknown" || finding.Authority != model.CompatibilityReduced || finding.RequirementID != "" {
			t.Fatalf("unexpected Exact finding: %+v", finding)
		}
		foundSkip := false
		for _, skip := range plan.Skipped {
			foundSkip = foundSkip || skip.Resource == finding.Target && strings.Contains(skip.Reason, "origin unavailable")
		}
		if !foundSkip {
			t.Fatalf("missing visible skip for %s: %+v", finding.Target, plan.Skipped)
		}
	}
	for _, operation := range plan.Operations {
		if operation.Action == "remove" {
			t.Fatalf("unknown origin authorized removal: %+v", operation)
		}
	}
	if verification := Verify(saved, current, VerifyOptions{Exact: true}); verification.OK || !reflect.DeepEqual(verification.Missing, []string{"aur:yay", "official:htop"}) {
		t.Fatalf("Exact verification falsely converged: %+v", verification)
	}
}

func TestRestoreCompatibilityMetadataRequirementLinksFinding(t *testing.T) {
	saved := profile.Packages{Official: []string{"alacritty"}}
	current := freshCurrent(nil)
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Requirements) != 1 || plan.Requirements[0].ID != "packages.metadata" {
		t.Fatalf("missing readiness requirement: %+v", plan.Requirements)
	}
	got, err := RestoreCompatibility(saved, current, map[string]bool{"official:alacritty": true}, false, plan.Requirements)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.CompatibilityUnknown || got.Authority != model.CompatibilityBlocked || len(got.Findings) != 1 || got.Findings[0].RequirementID != "packages.metadata" || got.Findings[0].Code != "packages.origin.unknown" {
		t.Fatalf("missing install metadata = %+v", got)
	}
}

func TestRestoreCompatibilityKnownOriginCanBeSupportedForPackageAuthority(t *testing.T) {
	saved := profile.Packages{Official: []string{"git"}}
	current := profile.Packages{Official: []string{"git"}, Installed: []string{"git"}}
	got, err := RestoreCompatibility(saved, current, map[string]bool{"official:git": true}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.CompatibilitySupported || got.Authority != model.CompatibilityUnchanged || len(got.Evidence) == 0 || len(got.Findings) != 0 {
		t.Fatalf("known package origin = %+v", got)
	}
}

func TestRestoreCompatibilitySkipsUnselectedTargets(t *testing.T) {
	saved := profile.Packages{Absent: []profile.PackageAbsence{{Ref: "official:htop"}}}
	current := freshCurrent([]string{"htop"}, "htop")
	got, err := RestoreCompatibility(saved, current, map[string]bool{"official:htop": false}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Applies || got.State != "" || got.Authority != model.CompatibilityUnchanged || len(got.Findings) != 0 {
		t.Fatalf("skipped intent contributed compatibility authority: %+v", got)
	}
}

func TestRestoreCompatibilityDoesNotProbeOrMutate(t *testing.T) {
	fake := newFakePacman(t, []string{"core"}, nil, "git\n", "git\n")
	current, err := (Provider{Runner: command.SystemRunner{}}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := fake.commands(t)
	_, err = RestoreCompatibility(profile.Packages{Official: []string{"git"}}, current, map[string]bool{"official:git": true}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if after := fake.commands(t); after != before {
		t.Fatalf("compatibility ran additional commands (including possible sync/update/sudo): before=%q after=%q", before, after)
	}
}

func TestRestoreCompatibilityRequiredPreinstallMissingFromCatalogueBlocks(t *testing.T) {
	saved := profile.Packages{Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{"foo": true}}}
	current := profile.Packages{Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{"bar": false}}}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "5.0")
	if err != nil || len(plan.Skipped) == 0 || !strings.Contains(plan.Skipped[0].Reason, "not present") {
		t.Fatalf("missing preinstall must be skipped: plan=%+v err=%v", plan, err)
	}
	got, err := RestoreCompatibility(saved, current, map[string]bool{"preinstall:foo": true, "preinstalls": false}, false, plan.Requirements)
	if err != nil || got.State != model.CompatibilityIncompatible || got.Authority != model.CompatibilityBlocked || len(got.Findings) != 1 || got.Findings[0].Target != "preinstall:foo" || got.Findings[0].Code != "packages.preinstall.unavailable" {
		t.Fatalf("required missing preinstall falsely supported: %+v err=%v", got, err)
	}
}

func TestRestoreCompatibilitySkippedMissingPreinstallDoesNotBlock(t *testing.T) {
	saved := profile.Packages{Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{"foo": true}}}
	current := profile.Packages{Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{}}}
	got, err := RestoreCompatibility(saved, current, map[string]bool{"preinstall:foo": false, "preinstalls": false}, false, nil)
	if err != nil || got.Applies || len(got.Findings) != 0 || got.State != "" || got.Authority != model.CompatibilityUnchanged {
		t.Fatalf("policy-skipped missing preinstall blocks: %+v err=%v", got, err)
	}
}

func TestRestoreCompatibilityPreinstallInstallLinksMetadataRequirement(t *testing.T) {
	saved := profile.Packages{Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{"foo": true}}}
	current := freshCurrent(nil)
	current.Preinstalls = profile.Preinstalls{Managed: true, Items: map[string]bool{"foo": false}}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "5.0")
	if err != nil || len(plan.Requirements) != 1 || !reflect.DeepEqual(plan.Requirements[0].Operations, []string{"packages.preinstall.install.foo"}) {
		t.Fatalf("preinstall install must need metadata: plan=%+v err=%v", plan, err)
	}
	got, err := RestoreCompatibility(saved, current, map[string]bool{"preinstall:foo": true, "preinstalls": false}, false, plan.Requirements)
	if err != nil || got.State != model.CompatibilityUnknown || got.Authority != model.CompatibilityBlocked || len(got.Findings) != 1 || got.Findings[0].RequirementID != "packages.metadata" || got.Findings[0].Target != "preinstall:foo" {
		t.Fatalf("metadata-gated preinstall install falsely supported: %+v err=%v", got, err)
	}
}

func TestRestoreCompatibilityPreinstallGroupInstallLinksMetadataRequirement(t *testing.T) {
	saved := profile.Packages{Preinstalls: profile.Preinstalls{Managed: true, RemovedAll: false}}
	current := freshCurrent(nil)
	current.Preinstalls = profile.Preinstalls{Managed: true, RemovedAll: true}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "5.0")
	if err != nil || len(plan.Requirements) != 1 || !reflect.DeepEqual(plan.Requirements[0].Operations, []string{"packages.preinstalls.install"}) {
		t.Fatalf("group install must need metadata: plan=%+v err=%v", plan, err)
	}
	got, err := RestoreCompatibility(saved, current, map[string]bool{"preinstalls": true}, false, plan.Requirements)
	if err != nil || got.State != model.CompatibilityUnknown || got.Authority != model.CompatibilityBlocked || len(got.Findings) != 1 || got.Findings[0].RequirementID != "packages.metadata" || got.Findings[0].Target != "preinstalls" {
		t.Fatalf("metadata-gated preinstall group falsely supported: %+v err=%v", got, err)
	}
}

func TestRestoreCompatibilityDesiredFalseAbsentPreinstallNeedsNoInstall(t *testing.T) {
	saved := profile.Packages{Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{"foo": false}}}
	current := profile.Packages{Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{}}}
	got, err := RestoreCompatibility(saved, current, map[string]bool{"preinstall:foo": true, "preinstalls": false}, false, nil)
	if err != nil || got.State != model.CompatibilitySupported || got.Authority != model.CompatibilityUnchanged || len(got.Findings) != 0 {
		t.Fatalf("absent desired-false preinstall mistaken for missing install: %+v err=%v", got, err)
	}
	if verification := Verify(saved, current); !verification.OK {
		t.Fatalf("desired-false absent preinstall is already satisfied: %+v", verification)
	}
}
