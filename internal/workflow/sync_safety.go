package workflow

import "github.com/Grenco/omarchy-blueprint/internal/syncengine"

func syncDesiredSafetyBlockers(item syncengine.Item, live TargetInspection, evidence []SyncDesiredInspection) []syncengine.Blocker {
	var out []syncengine.Blocker
	for _, action := range item.Actions {
		one := item
		one.Actions = []syncengine.PlannedAction{action}
		if action != syncengine.ActionRestore {
			out = append(out, syncSafetyBlockers(one, live)...)
			continue
		}
		// Equal intent in different historical views can have different safety
		// constraints. Require every matching view to allow it; never let a
		// permissive local view shadow restrictive remote evidence.
		found := false
		codes := map[string]bool{}
		for _, e := range evidence {
			if e.Key != item.Key || !e.Desired.Equal(item.Desired) {
				continue
			}
			found = true
			for _, b := range syncSafetyBlockers(one, e.Inspection) {
				if !codes[b.Code] {
					out = append(out, b)
					codes[b.Code] = true
				}
			}
		}
		if !found {
			key := item.Key
			out = append(out, syncengine.Blocker{Code: "restore-evidence-missing", Message: "Selected desired intent lacks provider Restore safety evidence", Target: &key})
		}
	}
	unique := out[:0]
	seen := map[string]bool{}
	for _, b := range out {
		if !seen[b.Code] {
			unique = append(unique, b)
			seen[b.Code] = true
		}
	}
	return unique
}

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
