package workflow

import (
	"context"
	"fmt"
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
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
		differs, err := targetDifferences(ctx, provider, s.profile, targets)
		if err != nil {
			return CaptureInspection{}, fmt.Errorf("compare %s targets: %w", provider.ID(), err)
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
				Outcome:    captureOutcomeFor(target, decision, differs(target.Key)),
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
// whatever is already tracked; a classification the provider says never
// actually yields a fresh captured value at all (see
// TargetCapabilities.NoActionableUpdate: Config's UnchangedBaseline/
// HistoricalBaseline are baseline-derived, not real user customization)
// StopManages any recorded desired state instead once enabled, whether that
// state was a captured value or a desired-absent tombstone -- a saved
// deletion tombstone whose file reappears matching the baseline is no less
// stale than a saved file that reverts to it, so both converge the same
// way; only a target with no desired state at all has nothing to drop.
// Otherwise the outcome follows from whether the target is currently
// present on the machine and whether it was already tracked. A
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
	return captureOutcomeFor(target, decision, true)
}

// captureOutcomeFor is captureOutcome with knowledge of whether a target
// present on both sides actually differs from its saved desired state. An
// unchanged tracked target is Noop: Capture would rewrite it identically.
func captureOutcomeFor(target TargetInspection, decision CaptureDecision, differs bool) CaptureOutcome {
	if !target.CaptureEligible {
		if target.Capabilities.DropsDesiredWhenIneligible && target.Desired != TargetUnknown {
			return CaptureOutcomeStopManaging
		}
		return CaptureOutcomeBlocked
	}
	if !decision.Capture {
		return CaptureOutcomePreserve
	}
	// NoActionableUpdate means the classification is baseline-derived, not
	// real user customization, regardless of whether the previously desired
	// state was a captured value or a deletion tombstone: enabling Capture
	// converges by dropping either one entirely, since neither carries any
	// meaningful customization to keep. This must be checked before the
	// presence switch below, not folded into its present-and-desired-present
	// case alone -- a saved ConfigDelete tombstone whose file reappears
	// matching the baseline is Current=Present/Desired=Absent, and would
	// otherwise fall through to the switch's Add case ("add it back") even
	// though real Capture drops the stale deletion intent the same way it
	// drops a stale captured value.
	if target.Capabilities.NoActionableUpdate && target.Desired != TargetUnknown {
		return CaptureOutcomeStopManaging
	}
	switch {
	case target.Current == TargetPresent && target.Desired == TargetPresent:
		if !differs {
			return CaptureOutcomeNoop
		}
		return CaptureOutcomeUpdate
	case target.Current == TargetPresent && target.Desired != TargetPresent:
		return CaptureOutcomeAdd
	case target.Current != TargetPresent && target.Desired == TargetUnknown && target.Capabilities.RecordsNewAbsence:
		return CaptureOutcomeAbsent
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

// ChangeTargetResolver is implemented by providers that can attribute each
// of their Diff changes to the policy target key it describes. It lets
// workflow relate factual differences to Capture/Restore policy without
// knowing any provider's target identity format. ok=false means the change
// cannot be attributed; ok=true with an empty key means the change concerns
// no policy target at all (e.g. Config's management settings).
type ChangeTargetResolver interface {
	ChangeTargetKey(change model.Change) (key string, ok bool)
}

// RestoreTargetResolver is implemented by providers that can attribute each
// operation in their Restore plan to the policy target key it acts on, so
// workflow can tell whether a difference is really a Restore candidate
// under the machine's effective Restore intent. ok=false means the
// operation cannot be attributed; ok=true with an empty key means it only
// supports other operations (e.g. reloading Hyprland) and targets nothing.
type RestoreTargetResolver interface {
	RestoreOperationTargetKey(op model.Operation) (key string, ok bool)
}

// targetDifferences reports, per target key, whether the provider's current
// state differs from saved desired state. Without a resolver, or when any
// change cannot be attributed to a target in this provider's inspected
// inventory, every target is conservatively treated as differing so a real
// change is never presented as "no action needed".
func targetDifferences(ctx context.Context, provider Provider, data profile.Data, targets []TargetInspection) (func(string) bool, error) {
	resolver, ok := provider.(ChangeTargetResolver)
	if !ok {
		return func(string) bool { return true }, nil
	}
	changes, err := provider.Diff(ctx, data)
	if err != nil {
		return nil, err
	}
	inventory := make(map[string]bool, len(targets))
	for _, target := range targets {
		inventory[target.Key] = true
	}
	changed := make(map[string]bool, len(changes))
	for _, change := range changes {
		key, ok := resolver.ChangeTargetKey(change)
		if !ok || (key != "" && !inventory[key]) {
			return func(string) bool { return true }, nil
		}
		if key != "" {
			changed[key] = true
		}
	}
	return func(key string) bool { return changed[key] }, nil
}

// CaptureReviewGroup is how a Capture preview is presented for approval.
type CaptureReviewGroup string

const (
	CaptureReviewChanges   CaptureReviewGroup = "Changes"
	CaptureReviewPreserved CaptureReviewGroup = "Preserved by policy"
	CaptureReviewBlocked   CaptureReviewGroup = "Blocked"
	CaptureReviewNoAction  CaptureReviewGroup = "No action needed"
)

var captureReviewOrder = []CaptureReviewGroup{CaptureReviewChanges, CaptureReviewPreserved, CaptureReviewBlocked, CaptureReviewNoAction}

// ReviewGroup places a target in the review. Preserve is only "by policy"
// when Capture policy disabled it; a provider keeping missing desired state
// on its own (Resources) needs no action rather than a policy explanation.
func (t CaptureTarget) ReviewGroup() CaptureReviewGroup {
	switch t.Outcome {
	case CaptureOutcomeAdd, CaptureOutcomeUpdate, CaptureOutcomeAbsent, CaptureOutcomeStopManaging:
		return CaptureReviewChanges
	case CaptureOutcomePreserve:
		if !t.Decision.Capture {
			return CaptureReviewPreserved
		}
		return CaptureReviewNoAction
	case CaptureOutcomeBlocked:
		return CaptureReviewBlocked
	default:
		return CaptureReviewNoAction
	}
}

// CaptureReviewSection is one non-empty review group, targets ordered by
// category and then target key.
type CaptureReviewSection struct {
	Group   CaptureReviewGroup
	Targets []CaptureTarget
}

// Review groups the inspection for approval in a fixed group order with a
// deterministic target order, omitting empty groups.
func (i CaptureInspection) Review() []CaptureReviewSection {
	buckets := map[CaptureReviewGroup][]CaptureTarget{}
	for _, targets := range i.Categories {
		for _, target := range targets {
			group := target.ReviewGroup()
			buckets[group] = append(buckets[group], target)
		}
	}
	sections := make([]CaptureReviewSection, 0, len(captureReviewOrder))
	for _, group := range captureReviewOrder {
		targets := buckets[group]
		if len(targets) == 0 {
			continue
		}
		sort.SliceStable(targets, func(a, b int) bool {
			if targets[a].Category != targets[b].Category {
				return targets[a].Category < targets[b].Category
			}
			return targets[a].Inspection.Key < targets[b].Inspection.Key
		})
		sections = append(sections, CaptureReviewSection{Group: group, Targets: targets})
	}
	return sections
}
