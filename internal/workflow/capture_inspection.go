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
// profile or any provider state. In PR 2, every eligible target resolves to
// the in-memory default-enabled decision (real policy resolution lands in
// PR 3); provider safety still blocks ineligible targets outright.
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
			decision := DefaultCaptureDecision()
			items = append(items, CaptureTarget{
				Category:   provider.ID(),
				Inspection: target,
				Policy:     defaultCapturePolicy(s.machine.Name, provider.ID(), target.Key, decision),
				Decision:   decision,
				Outcome:    captureOutcome(target, decision),
			})
		}
		categories[provider.ID()] = items
	}
	return CaptureInspection{Categories: categories}, nil
}

// defaultCapturePolicy represents the PR 2 compatibility policy view: no
// rules exist yet, so every target resolves to the provider default with no
// explicit source.
func defaultCapturePolicy(machine, category, target string, decision CaptureDecision) policy.EffectiveSetting {
	return policy.EffectiveSetting{
		Enabled:  decision.Capture,
		Explicit: false,
		Source: policy.Source{
			Kind:     policy.SourceDefault,
			Machine:  machine,
			Category: category,
			Target:   target,
		},
	}
}

// captureOutcome mirrors the Capture merge transitions PR 3 implements
// per-provider (see Task 17): a target ineligible for Capture is Blocked
// regardless of policy; Capture Disabled always Preserves whatever is
// already tracked; otherwise the outcome follows from whether the target is
// currently present on the machine and whether it was already tracked. A
// missing-but-desired target has three genuinely different transitions, not
// two: a provider that can record an explicit tombstone does so (Absent --
// Blueprint keeps managing the target and remembers its removal); one that
// cannot, but leaves prior desired state untouched, Preserves it (Resources:
// no way to tell "gone" from "not yet restored" apart); one that cannot and
// does not preserve it silently drops it from desired state entirely
// (StopManaging -- Defaults/Shell: Capture always writes a fresh full
// replacement, so an empty/vanished value carries no desired state
// afterward, not a remembered absence).
func captureOutcome(target TargetInspection, decision CaptureDecision) CaptureOutcome {
	if !target.CaptureEligible {
		return CaptureOutcomeBlocked
	}
	if !decision.Capture {
		return CaptureOutcomePreserve
	}
	switch {
	case target.Current == TargetPresent && target.Desired == TargetPresent:
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
