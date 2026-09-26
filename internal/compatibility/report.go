// Package compatibility builds and validates read-only Restore compatibility
// reports from provider-owned semantic evidence. It performs no system probes.
package compatibility

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func stateRank(state model.CompatibilityState) int {
	switch state {
	case model.CompatibilitySupported:
		return 1
	case model.CompatibilityUnknown:
		return 2
	case model.CompatibilityIncompatible:
		return 3
	default:
		return 0
	}
}

func authorityRank(authority model.CompatibilityAuthority) int {
	switch authority {
	case model.CompatibilityUnchanged:
		return 1
	case model.CompatibilityReduced:
		return 2
	case model.CompatibilityBlocked:
		return 3
	default:
		return 0
	}
}

func sortedEvidence(evidence []model.CompatibilityEvidence) []model.CompatibilityEvidence {
	if len(evidence) == 0 {
		return nil
	}
	out := append([]model.CompatibilityEvidence(nil), evidence...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Summary < out[j].Summary
	})
	return out
}

func sortedFindings(findings []model.CompatibilityFinding) []model.CompatibilityFinding {
	if len(findings) == 0 {
		return nil
	}
	out := append([]model.CompatibilityFinding(nil), findings...)
	for i := range out {
		out[i].Evidence = sortedEvidence(out[i].Evidence)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// BuildCategory conservatively aggregates provider evidence for its effective
// Apply intent. A provider with no Apply intent must not claim compatibility.
func BuildCategory(category string, applies bool, evidence []model.CompatibilityEvidence, findings []model.CompatibilityFinding) (model.CompatibilityCategory, error) {
	if strings.TrimSpace(category) == "" {
		return model.CompatibilityCategory{}, fmt.Errorf("compatibility: missing category")
	}
	result := model.CompatibilityCategory{Category: category, Applies: applies, Authority: model.CompatibilityUnchanged}
	if !applies {
		if len(evidence) != 0 || len(findings) != 0 {
			return model.CompatibilityCategory{}, fmt.Errorf("compatibility: non-applicable %s contains evidence or findings", category)
		}
		return result, nil
	}
	if len(evidence) == 0 && len(findings) == 0 {
		return model.CompatibilityCategory{}, fmt.Errorf("compatibility: %s has no affirmative evidence or finding", category)
	}
	for _, item := range evidence {
		if strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.Summary) == "" {
			return model.CompatibilityCategory{}, fmt.Errorf("compatibility: %s has incomplete evidence", category)
		}
	}
	result.Evidence = sortedEvidence(evidence)
	result.Findings = sortedFindings(findings)
	result.State = model.CompatibilitySupported
	for _, item := range result.Findings {
		if item.Code == "" || strings.TrimSpace(item.Summary) == "" || stateRank(item.State) == 0 || authorityRank(item.Authority) == 0 {
			return model.CompatibilityCategory{}, fmt.Errorf("compatibility: %s has invalid finding %q", category, item.Code)
		}
		if item.State == model.CompatibilityIncompatible && item.Authority != model.CompatibilityBlocked {
			return model.CompatibilityCategory{}, fmt.Errorf("compatibility: incompatible %s/%s must block", category, item.Code)
		}
		for _, detail := range item.Evidence {
			if strings.TrimSpace(detail.Kind) == "" || strings.TrimSpace(detail.Summary) == "" {
				return model.CompatibilityCategory{}, fmt.Errorf("compatibility: %s/%s has incomplete evidence", category, item.Code)
			}
		}
		if stateRank(item.State) > stateRank(result.State) {
			result.State = item.State
		}
		if authorityRank(item.Authority) > authorityRank(result.Authority) {
			result.Authority = item.Authority
		}
	}
	return result, nil
}

// NormalizeReport makes approval comparison independent of map iteration and
// provider enumeration order, without mutating provider-owned input slices.
func NormalizeReport(report model.CompatibilityReport) model.CompatibilityReport {
	result := report
	result.Categories = make([]model.CompatibilityCategory, len(report.Categories))
	for i, category := range report.Categories {
		category.Evidence = sortedEvidence(category.Evidence)
		category.Findings = sortedFindings(category.Findings)
		result.Categories[i] = category
	}
	sort.Slice(result.Categories, func(i, j int) bool {
		return result.Categories[i].Category < result.Categories[j].Category
	})
	return result
}

// ValidateReport fails closed on incomplete or contradictory public planning
// data. targets is the selected providers' already-inspected target namespace.
func ValidateReport(report model.CompatibilityReport, selectedCategories []string, targets map[string]map[string]struct{}, requirements []model.Requirement) error {
	if !report.Target.Known || strings.TrimSpace(report.Target.OmarchyVersion) == "" {
		return fmt.Errorf("compatibility: current target environment must be known")
	}
	if report.ProfileLastCapture.Known {
		if strings.TrimSpace(report.ProfileLastCapture.OmarchyVersion) == "" {
			return fmt.Errorf("compatibility: known profile last capture needs an Omarchy version")
		}
	} else if report.ProfileLastCapture.OmarchyVersion != "" || report.ProfileLastCapture.OmarchyChannel != "" {
		return fmt.Errorf("compatibility: unknown profile last capture must omit version and channel")
	}
	selected := make(map[string]struct{}, len(selectedCategories))
	for _, category := range selectedCategories {
		if category == "" {
			return fmt.Errorf("compatibility: empty selected category")
		}
		if _, duplicate := selected[category]; duplicate {
			return fmt.Errorf("compatibility: duplicate selected category %q", category)
		}
		selected[category] = struct{}{}
	}
	knownRequirements := make(map[string]struct{}, len(requirements))
	for _, requirement := range requirements {
		if requirement.ID == "" {
			return fmt.Errorf("compatibility: requirement without ID")
		}
		if _, duplicate := knownRequirements[requirement.ID]; duplicate {
			return fmt.Errorf("compatibility: duplicate requirement %q", requirement.ID)
		}
		knownRequirements[requirement.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(report.Categories))
	for _, category := range report.Categories {
		if _, selectedHere := selected[category.Category]; !selectedHere {
			return fmt.Errorf("compatibility: unselected category %q", category.Category)
		}
		if _, duplicate := seen[category.Category]; duplicate {
			return fmt.Errorf("compatibility: duplicate category %q", category.Category)
		}
		seen[category.Category] = struct{}{}
		aggregate, err := BuildCategory(category.Category, category.Applies, category.Evidence, category.Findings)
		if err != nil {
			return err
		}
		if category.State != aggregate.State || category.Authority != aggregate.Authority {
			return fmt.Errorf("compatibility: inconsistent state or authority for %s", category.Category)
		}
		findingKeys := make(map[[2]string]struct{}, len(category.Findings))
		for _, finding := range category.Findings {
			key := [2]string{finding.Target, finding.Code}
			if _, duplicate := findingKeys[key]; duplicate {
				return fmt.Errorf("compatibility: duplicate finding %s/%s", category.Category, finding.Code)
			}
			findingKeys[key] = struct{}{}
			if finding.Target != "" {
				if _, known := targets[category.Category][finding.Target]; !known {
					return fmt.Errorf("compatibility: finding %s/%s has unknown target %q", category.Category, finding.Code, finding.Target)
				}
			}
			if finding.RequirementID != "" {
				if _, known := knownRequirements[finding.RequirementID]; !known {
					return fmt.Errorf("compatibility: finding %s/%s references unknown requirement %q", category.Category, finding.Code, finding.RequirementID)
				}
			}
		}
	}
	for category := range selected {
		if _, exists := seen[category]; !exists {
			return fmt.Errorf("compatibility: selected category %q has no assessment", category)
		}
	}
	return nil
}

// BlockingFindings returns only findings that prevent applying the selected
// scope, in deterministic category/target/code order.
func BlockingFindings(report model.CompatibilityReport) []model.CompatibilityFinding {
	var findings []model.CompatibilityFinding
	for _, category := range NormalizeReport(report).Categories {
		if !category.Applies {
			continue
		}
		for _, finding := range category.Findings {
			if finding.Authority == model.CompatibilityBlocked {
				findings = append(findings, finding)
			}
		}
	}
	return findings
}
