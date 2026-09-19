package workflow

import (
	"context"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

// CaptureOutcome is the read-only, descriptive result InspectCapture reports
// for one target. It never performs a mutation and must stay consistent with
// what an actual Capture of the same target would do.
type CaptureOutcome string

const (
	CaptureOutcomeAdd      CaptureOutcome = "add"
	CaptureOutcomeUpdate   CaptureOutcome = "update"
	CaptureOutcomeAbsent   CaptureOutcome = "absent"
	CaptureOutcomePreserve CaptureOutcome = "preserve"
	CaptureOutcomeBlocked  CaptureOutcome = "blocked"
	CaptureOutcomeNoop     CaptureOutcome = "noop"
	// CaptureOutcomeStopManaging is distinct from CaptureOutcomeAbsent: Absent
	// means Blueprint keeps managing the target and records an explicit
	// tombstone remembering its removal; StopManaging means Blueprint carries
	// no desired state for the target at all afterward (e.g. Defaults: an
	// empty captured value is not a remembered absence, it is simply
	// unmanaged).
	CaptureOutcomeStopManaging CaptureOutcome = "stop-managing"
)

// CaptureTarget is one provider-defined target's read-only Capture preview:
// what a Capture run would do to it, and why.
type CaptureTarget struct {
	Category   string
	Inspection TargetInspection
	Policy     policy.EffectiveSetting
	Decision   CaptureDecision
	Outcome    CaptureOutcome
}

// CaptureInspection is a whole-session, read-only Capture preview grouped by
// provider category.
type CaptureInspection struct {
	Categories map[string][]CaptureTarget
}

// InspectCapture previews what a Capture run would do without mutating the
// profile or any provider state, resolving each target's real effective
// Capture policy against the session's currently selected machine; provider
// safety still blocks ineligible targets outright regardless of policy.
func (s *Session) InspectCapture(ctx context.Context, onlyProvider string) (CaptureInspection, error) {
	providers := s.providers
	if onlyProvider != "" {
		provider, ok := ProviderByID(s.providers, onlyProvider)
		if !ok {
			return CaptureInspection{}, fmt.Errorf("unknown category %s", onlyProvider)
		}
		providers = []Provider{provider}
	}
	categories := make(map[string][]CaptureTarget, len(providers))
	for _, provider := range providers {
		if onlyProvider == "" {
			if category, ok := provider.(categoryProvider); ok && !category.CategoryEnabled() {
				continue
			}
		}
		targets, err := provider.InspectTargets(ctx, s.profile)
		if err != nil {
			return CaptureInspection{}, fmt.Errorf("inspect %s targets: %w", provider.ID(), err)
		}
		if len(targets) == 0 {
			continue
		}
		items := make([]CaptureTarget, 0, len(targets))
		for _, target := range targets {
			effective, decision, err := s.resolveCaptureTarget(ctx, provider.ID(), target)
			if err != nil {
				return CaptureInspection{}, fmt.Errorf("resolve %s policy for %s: %w", provider.ID(), target.Key, err)
			}
			items = append(items, CaptureTarget{
				Category:   provider.ID(),
				Inspection: target,
				Policy:     effective,
				Decision:   decision,
				Outcome:    captureOutcome(target, decision),
			})
		}
		categories[provider.ID()] = items
	}
	return CaptureInspection{Categories: categories}, nil
}

// captureOutcome mirrors the Capture merge transitions PR 3 implements
// per-provider (see Task 17): a target ineligible for Capture is Blocked
// regardless of policy -- unless the provider says ineligibility here is an
// ownership-management transition, not a safety freeze (see
// TargetCapabilities.DropsDesiredWhenIneligible), in which case any
// previously recorded desired state -- present or an explicit desired-absent
// tombstone alike -- actively loses it (StopManaging), same as it really
// will under Capture; a target with no recorded desired state at all has
// nothing to drop, so it stays Blocked. Capture Disabled always Preserves
// whatever is already tracked; otherwise the outcome follows from whether
// the target is currently present on the machine and whether it was already
// tracked. A present-and-desired-present target normally means a genuinely
// fresh value would be captured (Update) -- unless the provider says its
// classification can never actually produce one (see
// TargetCapabilities.NoActionableUpdate: Config's UnchangedBaseline/
// HistoricalBaseline are baseline-derived, not real user customization, so
// enabling Capture converges by dropping the stale value instead). A
// missing-but-desired target has three genuinely different transitions, not
// two: a provider that can record an explicit tombstone does so (Absent --
// Blueprint keeps managing the target and remembers its removal); one that
// cannot, but leaves prior desired state untouched, Preserves it
// (Resources: no way to tell "gone" from "not yet restored" apart); one
// that cannot and does not preserve it silently drops it from desired state
// entirely (StopManaging -- Defaults/Shell: Capture always writes a fresh
// full replacement, so an empty/vanished value carries no desired state
// afterward, not a remembered absence).
func captureOutcome(target TargetInspection, decision CaptureDecision) CaptureOutcome {
	if !target.CaptureEligible {
		if target.Capabilities.DropsDesiredWhenIneligible && target.Desired != TargetUnknown {
			return CaptureOutcomeStopManaging
		}
		return CaptureOutcomeBlocked
	}
	if !decision.Capture {
		return CaptureOutcomePreserve
	}
	switch {
	case target.Current == TargetPresent && target.Desired == TargetPresent:
		if target.Capabilities.NoActionableUpdate {
			return CaptureOutcomeStopManaging
		}
		return CaptureOutcomeUpdate
	case target.Current == TargetPresent && target.Desired != TargetPresent:
		return CaptureOutcomeAdd
	case target.Current != TargetPresent && target.Desired == TargetPresent:
		if target.Capabilities.SupportsDesiredAbsence {
			return CaptureOutcomeAbsent
		}
		if target.Capabilities.PreservesMissingDesired {
			return CaptureOutcomePreserve
		}
		return CaptureOutcomeStopManaging
	default:
		return CaptureOutcomeNoop
	}
}
