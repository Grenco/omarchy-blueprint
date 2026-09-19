package app

import (
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// restoreSkip is one target key's resolved Restore Skip decision, carrying
// the reason a provider should attach to the model.Skipped entry it records
// for it.
type restoreSkip struct {
	Key, Reason string
}

// resolveRestoreSkip resolves key's RestoreContext decision via Require,
// failing closed (a non-nil error) rather than treating a missing or
// unresolved decision as an implicit Apply. RestoreContext.Require's own
// contract is that PR 4+ must reject these before Restore planning uses
// the decision: an inventory/orchestration omission (a provider-owned
// desired target key with no corresponding InspectTargets entry, or a
// decision workflow never marked Resolved) must never silently broaden
// Restore's authority by falling through as "not skipped." Every provider
// filter function calls this once per desired target key it owns --
// present state and desired-absent tombstones alike -- rather than
// consulting a permissive lookup map built from whatever restoreCtx.Targets
// happens to already contain.
//
// Returns (skip, entry, nil) when resolved: skip is true and entry is
// populated only when the decision says Restore Skip; skip is false
// (entry zero) when it says Apply.
func resolveRestoreSkip(restoreCtx workflow.RestoreContext, provider, key string) (bool, restoreSkip, error) {
	decision, err := restoreCtx.Require(key)
	if err != nil {
		return false, restoreSkip{}, fmt.Errorf("%s: %w", provider, err)
	}
	if decision.Restore {
		return false, restoreSkip{}, nil
	}
	return true, restoreSkip{Key: key, Reason: decision.Reason}, nil
}

// restoreSkipSet indexes skips by key for Resource-matching lookups.
func restoreSkipSet(skips []restoreSkip) map[string]string {
	set := make(map[string]string, len(skips))
	for _, skip := range skips {
		set[skip.Key] = skip.Reason
	}
	return set
}

// recordRestoreSkips is the visibility backstop for the design's "a target
// with Restore Skip must remain a visible skip/reason in the plan rather
// than silently disappearing where practical" invariant: it guarantees
// every key in skips appears exactly once in plan.Skipped with its resolved
// policy reason, after a provider has already filtered its own desired
// state so skip targets were never batched into an Operation in the first
// place. It still defends against an Operation slipping through (moving it
// to Skipped) and against the provider's own unrelated skip logic already
// having flagged the same resource for a different reason (its reason is
// replaced with the resolved policy reason, so exactly one entry exists per
// resource, not two disagreeing ones).
func recordRestoreSkips(plan model.RestorePlan, provider string, skips []restoreSkip) model.RestorePlan {
	if len(skips) == 0 {
		return plan
	}
	set := restoreSkipSet(skips)
	seen := make(map[string]bool, len(skips))
	operations := make([]model.Operation, 0, len(plan.Operations))
	for _, op := range plan.Operations {
		if reason, ok := set[op.Resource]; ok {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: provider, Resource: op.Resource, Reason: reason})
			seen[op.Resource] = true
			continue
		}
		operations = append(operations, op)
	}
	plan.Operations = operations
	for i := range plan.Skipped {
		if plan.Skipped[i].Provider != provider {
			continue
		}
		if reason, ok := set[plan.Skipped[i].Resource]; ok && !seen[plan.Skipped[i].Resource] {
			plan.Skipped[i].Reason = reason
			seen[plan.Skipped[i].Resource] = true
		}
	}
	for _, skip := range skips {
		if !seen[skip.Key] {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: provider, Resource: skip.Key, Reason: skip.Reason})
		}
	}
	return plan
}
