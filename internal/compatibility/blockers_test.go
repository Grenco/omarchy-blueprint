package compatibility

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func freshMachinePlan(packages int) model.RestorePlan {
	pkgs := model.CompatibilityCategory{Category: "packages", Applies: true, State: model.CompatibilityUnknown, Authority: model.CompatibilityBlocked}
	for i := range packages {
		pkgs.Findings = append(pkgs.Findings, model.CompatibilityFinding{Code: "packages.origin.unknown", Target: fmt.Sprintf("official:pkg-%03d", i), State: model.CompatibilityUnknown, Authority: model.CompatibilityBlocked, Summary: "databases missing", RequirementID: "packages.metadata"})
	}
	pkgs.Findings = append(pkgs.Findings, model.CompatibilityFinding{Code: "packages.origin.unknown", Target: "official:installed", State: model.CompatibilityUnknown, Authority: model.CompatibilityUnchanged, Summary: "databases missing"})
	services := model.CompatibilityCategory{Category: "services", Applies: true, State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Findings: []model.CompatibilityFinding{
		{Code: "services.unit.invalid", Target: "a.service", State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Summary: "systemd rejected this service: Command /usr/bin/a is not executable"},
		{Code: "services.unit.invalid", Target: "b.service", State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Summary: "blocked because systemd validates services together and rejected a.service"},
		{Code: "services.unit.invalid", Target: "c.service", State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Summary: "blocked because systemd validates services together and rejected a.service"},
	}}
	themes := model.CompatibilityCategory{Category: "themes", Applies: true, State: model.CompatibilitySupported, Authority: model.CompatibilityUnchanged}
	return model.RestorePlan{
		Compatibility: model.CompatibilityReport{Categories: []model.CompatibilityCategory{pkgs, services, themes}},
		Requirements:  []model.Requirement{{ID: "packages.metadata", Provider: "packages", Remediation: []string{"omarchy", "update"}}},
	}
}

func TestBlockerGroupsCollapseOneLinePerTargetIntoCauses(t *testing.T) {
	groups := BlockerGroups(freshMachinePlan(200))
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want packages + two services causes: %+v", len(groups), groups)
	}
	if groups[0].Category != "packages" || len(groups[0].Targets) != 200 || groups[0].Requirement == nil || groups[0].Requirement.Remediation[1] != "update" {
		t.Fatalf("packages group = %+v, want 200 blocked targets linked to omarchy update", groups[0])
	}
	if !reflect.DeepEqual(groups[1].Targets, []string{"a.service"}) || !reflect.DeepEqual(groups[2].Targets, []string{"b.service", "c.service"}) {
		t.Fatalf("services groups = %+v, want the rejected unit apart from the units it blocked", groups[1:])
	}
	for _, group := range groups {
		if group.Authority != model.CompatibilityBlocked {
			t.Fatalf("non-blocking finding grouped as a blocker: %+v", group)
		}
	}
}

func TestNotReadyCategoriesIncludesRequirementOnlyCategories(t *testing.T) {
	plan := freshMachinePlan(1)
	plan.Requirements = append(plan.Requirements, model.Requirement{ID: "themes.thing", Provider: "themes"})
	if got := NotReadyCategories(plan); !reflect.DeepEqual(got, []string{"packages", "services", "themes"}) {
		t.Fatalf("NotReadyCategories = %v", got)
	}
	if got := NotReadyCategories(model.RestorePlan{}); got != nil {
		t.Fatalf("ready plan reports %v", got)
	}
}
