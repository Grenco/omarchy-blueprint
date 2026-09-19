package workflow

import (
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

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
	// PreservesMissingDesired reports what Capture does when it finds a
	// target currently missing but previously desired, and
	// SupportsDesiredAbsence is false (so no explicit tombstone is
	// possible): true means Capture leaves the existing desired state
	// untouched (Resources: no way to tell "gone" from "not yet detected"
	// apart, so never overwrite); false (the default) means Capture
	// silently stops managing it, clearing the desired value on the next
	// Capture Update (Defaults, Shell: Capture always writes a fresh full
	// replacement, so a vanished value is dropped, not remembered). This
	// field is meaningless when SupportsDesiredAbsence is true, since a
	// provider that can record explicit absence records it instead.
	PreservesMissingDesired bool
	// DropsDesiredWhenIneligible distinguishes two different reasons a
	// target can be CaptureEligible: false. The default (false) means a
	// safety freeze: Capture leaves whatever is already desired completely
	// untouched, so the CaptureOutcomeBlocked preview is accurate -- nothing
	// changes. true means an intentional ownership-management transition
	// instead (Config: Delegated to a stronger owner, or explicitly
	// Excluded): Capture actively drops the target's desired state
	// regardless of policy, so reporting it as "blocked" (implying nothing
	// happens) would be misleading; captureOutcome reports
	// CaptureOutcomeStopManaging for it instead whenever the target has any
	// recorded desired state, present or an explicit desired-absent
	// tombstone alike, since real Capture drops both the same way.
	DropsDesiredWhenIneligible bool
	// NoActionableUpdate reports that this target's classification means
	// Capture can never produce a genuinely fresh captured value for it,
	// even though it is currently present and desired present (Config's
	// UnchangedBaseline/HistoricalBaseline: the live bytes are
	// baseline-derived, not real user customization, so there is nothing
	// new to adopt). When true, captureOutcome reports
	// CaptureOutcomeStopManaging for the present-and-desired-present
	// transition instead of the default Update, matching what real Capture
	// does: enabling Capture converges by dropping the stale desired value
	// rather than updating it, and Capture Disabled still Preserves it as
	// usual.
	NoActionableUpdate bool
}

// TargetInspection is one provider-defined policy target's read-only,
// descriptive state. Actual Capture and Restore must re-inspect before
// mutation rather than trusting a stale TargetInspection.
type TargetInspection struct {
	Key    string
	Parent string
	// Ancestors is the full ancestor chain for a Hierarchical target,
	// nearest parent first (matching policy.ResolveRequest.Ancestors).
	// Policy resolution needs the whole chain, not just Parent, to find the
	// nearest matching ancestor rule when the immediate parent has none.
	// Non-hierarchical providers leave this nil.
	Ancestors       []string
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

// DefaultCaptureDecision is the explicit compatibility decision
// orchestration must record for every target it inspects before real
// policy resolution is wired up (PR 3). It is deliberately not returned
// implicitly for a missing map key: a target with no recorded decision at
// all means no orchestration ever considered it, which callers must treat
// as fail-closed rather than as an implicit allow.
func DefaultCaptureDecision() CaptureDecision { return CaptureDecision{Capture: true, Resolved: false} }

// DefaultRestoreDecision is the explicit compatibility decision
// orchestration must record for every target it plans before real policy
// resolution is wired up (PR 4). See DefaultCaptureDecision.
func DefaultRestoreDecision() RestoreDecision { return RestoreDecision{Restore: true, Resolved: false} }

// CaptureContext carries the resolved Capture decision for every target a
// provider inspected, for one machine. Orchestration must populate an
// entry for every inspected target (DefaultCaptureDecision while policy
// resolution is not yet active, or a resolved decision once it is); a
// target absent from Targets means no orchestration ever considered it.
type CaptureContext struct {
	Machine string
	Targets map[string]CaptureDecision
}

// Lookup returns the recorded CaptureDecision for key and whether one was
// found. It never invents a default: a missing key is a signal that
// orchestration never resolved this target, which is a bug to surface, not
// an implicit allow.
func (c CaptureContext) Lookup(key string) (CaptureDecision, bool) {
	decision, ok := c.Targets[key]
	return decision, ok
}

// Require returns the recorded CaptureDecision for key, or an error if no
// decision was recorded or the recorded decision was never marked
// Resolved. PR 3+ must call this (or an equivalent check) before any
// Capture mutation driven by policy; it intentionally rejects the
// DefaultCaptureDecision compatibility value PR 2 records, since an
// unresolved decision must not become write authority.
func (c CaptureContext) Require(key string) (CaptureDecision, error) {
	decision, ok := c.Lookup(key)
	if !ok {
		return CaptureDecision{}, fmt.Errorf("workflow: no capture decision recorded for target %q", key)
	}
	if !decision.Resolved {
		return CaptureDecision{}, fmt.Errorf("workflow: capture decision for target %q is unresolved", key)
	}
	return decision, nil
}

// RestoreContext carries the resolved Restore decision for every target a
// provider is planning, plus the restore run's two independent intent
// axes, for one machine. Orchestration must populate an entry for every
// planned target (DefaultRestoreDecision while policy resolution is not
// yet active, or a resolved decision once it is); a target absent from
// Targets means no orchestration ever considered it.
type RestoreContext struct {
	Machine string
	Options policy.RestoreOptions
	Targets map[string]RestoreDecision
}

// Lookup returns the recorded RestoreDecision for key and whether one was
// found. It never invents a default: a missing key is a signal that
// orchestration never resolved this target, which is a bug to surface, not
// an implicit allow.
func (c RestoreContext) Lookup(key string) (RestoreDecision, bool) {
	decision, ok := c.Targets[key]
	return decision, ok
}

// Require returns the recorded RestoreDecision for key, or an error if no
// decision was recorded or the recorded decision was never marked
// Resolved. PR 4+ must call this (or an equivalent check) before any
// Restore planning driven by policy; it intentionally rejects the
// DefaultRestoreDecision compatibility value PR 2 records, since an
// unresolved decision must not become write authority.
func (c RestoreContext) Require(key string) (RestoreDecision, error) {
	decision, ok := c.Lookup(key)
	if !ok {
		return RestoreDecision{}, fmt.Errorf("workflow: no restore decision recorded for target %q", key)
	}
	if !decision.Resolved {
		return RestoreDecision{}, fmt.Errorf("workflow: restore decision for target %q is unresolved", key)
	}
	return decision, nil
}
