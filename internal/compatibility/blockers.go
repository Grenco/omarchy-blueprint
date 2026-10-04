package compatibility

import (
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// FindingGroup is the findings of one category that share a cause: the same
// code, state, authority, linked requirement and explanation. Restore
// surfaces present groups rather than one line per target (ADR 0028); the
// plan itself, and its JSON, still carry every finding.
type FindingGroup struct {
	Category      string
	Code          string
	State         model.CompatibilityState
	Authority     model.CompatibilityAuthority
	Summary       string
	RequirementID string
	Targets       []string
	// Requirement is the plan requirement the findings link, if any.
	Requirement *model.Requirement
}

// GroupFindings groups one category's findings by cause, in the order each
// cause first appears, with each group's targets sorted.
func GroupFindings(category model.CompatibilityCategory) []FindingGroup {
	type key struct {
		code, requirement, summary string
		state                      model.CompatibilityState
		authority                  model.CompatibilityAuthority
	}
	var groups []FindingGroup
	index := map[key]int{}
	for _, finding := range category.Findings {
		k := key{finding.Code, finding.RequirementID, finding.Summary, finding.State, finding.Authority}
		at, ok := index[k]
		if !ok {
			groups = append(groups, FindingGroup{Category: category.Category, Code: finding.Code, State: finding.State, Authority: finding.Authority, Summary: finding.Summary, RequirementID: finding.RequirementID})
			at = len(groups) - 1
			index[k] = at
		}
		if finding.Target != "" {
			groups[at].Targets = append(groups[at].Targets, finding.Target)
		}
	}
	for i := range groups {
		sort.Strings(groups[i].Targets)
	}
	return groups
}

// BlockerGroups returns the plan's blocking finding groups, categories in
// report order, each linked to its plan requirement when it has one.
func BlockerGroups(plan model.RestorePlan) []FindingGroup {
	requirements := make(map[string]model.Requirement, len(plan.Requirements))
	for _, requirement := range plan.Requirements {
		requirements[requirement.ID] = requirement
	}
	var blockers []FindingGroup
	for _, category := range plan.Compatibility.Categories {
		if !category.Applies || category.Authority != model.CompatibilityBlocked {
			continue
		}
		for _, group := range GroupFindings(category) {
			if group.Authority != model.CompatibilityBlocked {
				continue
			}
			if requirement, linked := requirements[group.RequirementID]; linked {
				group.Requirement = &requirement
			}
			blockers = append(blockers, group)
		}
	}
	return blockers
}

// NotReadyCategories lists, in report order, the categories that stop the
// plan from applying: those with blocking findings and those with unmet
// requirements. These are the categories a deferral would leave out.
func NotReadyCategories(plan model.RestorePlan) []string {
	required := map[string]bool{}
	for _, requirement := range plan.Requirements {
		required[requirement.Provider] = true
	}
	var categories []string
	for _, category := range plan.Compatibility.Categories {
		if required[category.Category] || category.Applies && category.Authority == model.CompatibilityBlocked {
			categories = append(categories, category.Category)
		}
	}
	return categories
}
