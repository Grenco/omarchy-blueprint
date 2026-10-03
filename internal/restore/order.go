package restore

import "github.com/Grenco/omarchy-blueprint/internal/model"

// AwaitingStepsLast moves steps that may wait for the person (AwaitsYou),
// and every step that depends on them, to the end of the plan, keeping
// relative order otherwise (ADR 0028). Everything else is done before
// Restore waits for anyone. Dependencies stay satisfied: a moved step's
// dependencies are either unmoved, and so earlier, or moved with it in
// their original order.
func AwaitingStepsLast(operations []model.Operation) []model.Operation {
	late := map[string]bool{}
	for _, op := range operations {
		if op.AwaitsYou != "" {
			late[op.ID] = true
			continue
		}
		for _, dependency := range op.DependsOn {
			if late[dependency] {
				late[op.ID] = true
				break
			}
		}
	}
	if len(late) == 0 {
		return operations
	}
	ordered := make([]model.Operation, 0, len(operations))
	for _, op := range operations {
		if !late[op.ID] {
			ordered = append(ordered, op)
		}
	}
	for _, op := range operations {
		if late[op.ID] {
			ordered = append(ordered, op)
		}
	}
	return ordered
}
