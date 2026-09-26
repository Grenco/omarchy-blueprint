package packages

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func operationsByID(plan model.RestorePlan) map[string]model.Operation {
	ops := map[string]model.Operation{}
	for _, op := range plan.Operations {
		ops[op.ID] = op
	}
	return ops
}

func TestSudoElevatingPackageOperationsAreInteractiveAndSayWhy(t *testing.T) {
	saved := profile.Packages{
		Official:    []string{"alacritty"},
		AUR:         []string{"yay-bin"},
		Absent:      []profile.PackageAbsence{{Ref: "official:htop"}, {Ref: "aur:paru-bin"}},
		Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{"obsidian": true, "pinta": false}},
	}
	current := profile.Packages{
		Official: []string{"htop"}, AUR: []string{"paru-bin"}, Installed: []string{"htop", "paru-bin", "pinta"},
		Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{"obsidian": false, "pinta": true}},
	}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.0", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	ops := operationsByID(plan)
	for _, id := range []string{"packages.install.official", "packages.install.aur.yay-bin", "packages.remove.official.htop",
		"packages.remove.aur.paru-bin", "packages.preinstall.install.obsidian", "packages.preinstall.remove.pinta"} {
		op, ok := ops[id]
		if !ok {
			t.Fatalf("missing %s in %#v", id, plan.Operations)
		}
		if !op.Interactive || !strings.Contains(op.Notice, "administrator") {
			t.Fatalf("%s must run at the terminal and explain authentication: %#v", id, op)
		}
	}
	if len(plan.Requirements) != 0 {
		t.Fatalf("classified machine needs no readiness: %#v", plan.Requirements)
	}
}

func TestMissingSyncMetadataMakesPackageReadinessAPlanRequirement(t *testing.T) {
	saved := profile.Packages{Official: []string{"alacritty", "git"}, AUR: []string{"yay-bin"},
		Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{"obsidian": true}}}
	current := freshCurrent([]string{"git"}, "git")
	current.Preinstalls = profile.Preinstalls{Managed: true, Items: map[string]bool{"obsidian": false}}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.0")
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Requirement{{
		ID: "packages.metadata", Provider: "packages", Kind: "package-metadata",
		Reason:      "package metadata unavailable: pacman sync databases missing for core, extra; installing packages needs Omarchy's own system update first",
		Remediation: []string{"omarchy", "update"},
		Operations:  []string{"packages.install.aur.yay-bin", "packages.install.official", "packages.preinstall.install.obsidian"},
	}}
	if !reflect.DeepEqual(plan.Requirements, want) {
		t.Fatalf("requirements = %#v", plan.Requirements)
	}
	ops := operationsByID(plan)
	if op := ops["packages.install.official"]; !reflect.DeepEqual(op.Items, []string{"alacritty"}) || !op.Interactive {
		t.Fatalf("install stays visible and unchanged: %#v", op)
	}
	for _, op := range plan.Operations {
		for _, arg := range op.Command {
			if arg == "-Sy" || arg == "update" || arg == "-Syu" {
				t.Fatalf("Blueprint must never sync metadata itself: %#v", op)
			}
		}
	}
}

func TestReadinessIsOnlyRequiredWhenAnOperationNeedsMetadata(t *testing.T) {
	saved := profile.Packages{Official: []string{"git"}, Absent: []profile.PackageAbsence{{Ref: "official:htop"}}}
	current := freshCurrent([]string{"git", "htop"}, "git", "htop")
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.0", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Requirements) != 0 {
		t.Fatalf("nothing to install, so nothing needs metadata: %#v", plan.Requirements)
	}
}
