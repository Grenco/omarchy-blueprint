package workflow

import "github.com/Grenco/omarchy-blueprint/internal/policy"

// TargetState is the presence of a policy target's desired or current
// state as a provider reports it during read-only inspection.
type TargetState string

const (
	TargetUnknown TargetState = "unknown"
	TargetPresent TargetState = "present"
	TargetAbsent  TargetState = "absent"
)

// TargetCapabilities describes what a provider can possibly do for a
// target; it does not grant permission. Provider safety checks remain
// authoritative at planning time.
type TargetCapabilities struct {
	SupportsCapture        bool
	SupportsRestore        bool
	SupportsDesiredAbsence bool
	SupportsExactRemoval   bool
	Hierarchical           bool
}

// TargetInspection is one provider-defined policy target's read-only,
// descriptive state. Actual Capture and Restore must re-inspect before
// mutation rather than trusting a stale TargetInspection.
type TargetInspection struct {
	Key             string
	Parent          string
	Label           string
	Desired         TargetState
	Current         TargetState
	CaptureEligible bool
	RestoreEligible bool
	Capabilities    TargetCapabilities
	SafetyReason    string
}

// CaptureDecision is the resolved, immutable Capture outcome for one
// target, handed to providers instead of raw policy rules.
type CaptureDecision struct {
	Capture  bool
	Resolved bool
}

// RestoreDecision is the resolved, immutable Restore outcome for one
// target, handed to providers instead of raw policy rules.
type RestoreDecision struct {
	Restore  bool
	Resolved bool
}

// CaptureContext carries the resolved Capture decision for every target a
// provider inspected, for one machine.
type CaptureContext struct {
	Machine string
	Targets map[string]CaptureDecision
}

// Decision returns the resolved CaptureDecision for key, or the
// compatibility default (Capture enabled, Resolved false) when key is
// absent. The default lets PR 2 wire providers through CaptureContext
// before policy persistence and resolution activate in PR 3.
func (c CaptureContext) Decision(key string) CaptureDecision {
	if decision, ok := c.Targets[key]; ok {
		return decision
	}
	return CaptureDecision{Capture: true, Resolved: false}
}

// RestoreContext carries the resolved Restore decision for every target a
// provider is planning, plus the restore run's two independent intent
// axes, for one machine.
type RestoreContext struct {
	Machine string
	Options policy.RestoreOptions
	Targets map[string]RestoreDecision
}

// Decision returns the resolved RestoreDecision for key, or the
// compatibility default (Restore enabled, Resolved false) when key is
// absent. The default lets PR 2 wire providers through RestoreContext
// before policy persistence and resolution activate in PR 4.
func (c RestoreContext) Decision(key string) RestoreDecision {
	if decision, ok := c.Targets[key]; ok {
		return decision
	}
	return RestoreDecision{Restore: true, Resolved: false}
}
