package workflow

import "github.com/Grenco/omarchy-blueprint/internal/syncengine"

// Safety evidence does not rewrite policy or grant execution authority.
// Restore still constructs and validates a fresh plan in the execution slice.
func syncSafetyBlockers(item syncengine.Item, t TargetInspection) []syncengine.Blocker {
	var out []syncengine.Blocker
	add := func(code, message string) {
		key := item.Key
		out = append(out, syncengine.Blocker{Code: code, Message: message, Target: &key})
	}
	for _, action := range item.Actions {
		switch action {
		case syncengine.ActionCapture:
			if !t.CaptureEligible {
				add("capture-ineligible", "Provider safety withholds Capture for this target")
			}
		case syncengine.ActionRestore:
			if !t.RestoreEligible {
				add("restore-ineligible", "Provider safety withholds Restore for this target")
			}
		}
	}
	if len(item.Actions) != 0 && (t.RequiresSelection || t.ReviewRemoval) {
		add("target-review-required", "Provider ownership/adoption/removal requires explicit review")
	}
	return out
}
