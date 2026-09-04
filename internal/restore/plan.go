package restore

import (
	"fmt"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// ValidatePlan rejects invalid restore-plan dependency graphs before they are
// displayed or executed: blank or duplicate operation IDs, blank dependency
// IDs, unknown dependencies, and forward dependencies. Because every
// dependency must point to an earlier operation, cycles are impossible by
// construction.
func ValidatePlan(plan model.RestorePlan) error {
	seen := map[string]int{}
	for i, op := range plan.Operations {
		if strings.TrimSpace(op.ID) == "" {
			return fmt.Errorf("restore operation %d has empty id", i)
		}
		if _, ok := seen[op.ID]; ok {
			return fmt.Errorf("duplicate restore operation id %q", op.ID)
		}
		for _, dep := range op.DependsOn {
			if strings.TrimSpace(dep) == "" {
				return fmt.Errorf("operation %q has empty dependency", op.ID)
			}
			if _, ok := seen[dep]; !ok {
				return fmt.Errorf(
					"operation %q depends on unknown or later operation %q",
					op.ID, dep,
				)
			}
		}
		seen[op.ID] = i
	}
	return nil
}
