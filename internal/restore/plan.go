package restore

import (
	"encoding/hex"
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
	for _, op := range plan.Operations {
		if err := validateOperationAction(op); err != nil {
			return err
		}
	}
	return nil
}

func validateOperationAction(op model.Operation) error {
	actions := 0
	if len(op.Command) > 0 {
		actions++
	}
	if op.Copy != nil {
		actions++
	}
	if op.File != nil {
		actions++
	}
	if op.Delete != nil {
		actions++
	}
	if op.Directory != nil {
		actions++
	}
	if op.Symlink != nil {
		actions++
	}
	if op.GitPatch != nil {
		actions++
	}
	if actions != 1 {
		return fmt.Errorf("operation %s must contain exactly one command, copy, file, delete, directory, symlink, or git patch action", op.ID)
	}
	if op.Delete != nil {
		return validateFileDelete(op.ID, *op.Delete)
	}
	if op.File != nil {
		return validateFileWrite(op.ID, *op.File)
	}
	if op.GitPatch != nil {
		return validateGitPatch(op.ID, *op.GitPatch)
	}
	return nil
}

func validateGitPatch(operation string, action model.GitPatchApply) error {
	if strings.TrimSpace(action.Repository) == "" {
		return fmt.Errorf("git patch repository is required: %s", operation)
	}
	if strings.TrimSpace(action.Source) == "" {
		return fmt.Errorf("git patch source is required: %s", operation)
	}
	if len(action.SourceHash) != 64 {
		return fmt.Errorf("git patch source hash is invalid: %s", operation)
	}
	if _, err := hex.DecodeString(action.SourceHash); err != nil {
		return fmt.Errorf("git patch source hash is invalid: %s", operation)
	}
	return nil
}

func validateFileWrite(operation string, action model.FileWrite) error {
	if action.ReplaceExisting {
		if action.ExpectedMissing || action.ExpectedExisting == nil || !action.Backup {
			return fmt.Errorf("file replacement requires existing precondition and backup: %s", operation)
		}
		switch action.ExpectedExisting.Type {
		case "file", "directory", "symlink":
		default:
			return fmt.Errorf("file replacement precondition has invalid type %q: %s", action.ExpectedExisting.Type, operation)
		}
		return nil
	}
	if action.ExpectedExisting != nil {
		return fmt.Errorf("file write existing-object precondition requires replacement: %s", operation)
	}
	return nil
}

func validateFileDelete(operation string, action model.FileDelete) error {
	if strings.TrimSpace(action.Destination) == "" {
		return fmt.Errorf("file delete destination is required: %s", operation)
	}
	if action.ExpectedMissing && action.ExpectedExisting != nil {
		return fmt.Errorf("file delete requires exactly one destination precondition: %s", operation)
	}
	if !action.ExpectedMissing && action.ExpectedExisting == nil {
		return fmt.Errorf("file delete requires exactly one destination precondition: %s", operation)
	}
	if action.ExpectedMissing && action.Backup {
		return fmt.Errorf("file delete cannot back up an expected-missing destination: %s", operation)
	}
	if action.ExpectedExisting != nil {
		switch action.ExpectedExisting.Type {
		case "file", "directory", "symlink":
		default:
			return fmt.Errorf("file delete precondition has invalid type %q: %s", action.ExpectedExisting.Type, operation)
		}
	}
	return nil
}
