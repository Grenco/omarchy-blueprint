package app

import (
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// restoreSkip is one target key's resolved Restore Skip decision, carrying
// the reason a provider should attach to the model.Skipped entry it records
// for it.
type restoreSkip struct {
	Key, Reason string
}

// restoreSkips extracts every target key restoreCtx resolved to Restore
// Skip, sorted by key for deterministic plan output. A provider consults
// this to exclude Restore-Skip targets from the desired state it feeds its
// own Plan/Verify logic -- filtering the input is the only way to reliably
// stop a provider whose Plan can batch several targets into one Operation
// (e.g. Packages' bulk official/AUR installs) from ever batching a
// Restore-Skip target together with an Apply one.
func restoreSkips(restoreCtx workflow.RestoreContext) []restoreSkip {
	skips := make([]restoreSkip, 0, len(restoreCtx.Targets))
	for key, decision := range restoreCtx.Targets {
		if !decision.Restore {
			skips = append(skips, restoreSkip{Key: key, Reason: decision.Reason})
		}
	}
	sort.Slice(skips, func(i, j int) bool { return skips[i].Key < skips[j].Key })
	return skips
}

// restoreSkipSet indexes skips by key for filtering lookups.
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
