package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

// Label names the outcome for people.
func (o CaptureOutcome) Label() string {
	switch o {
	case CaptureOutcomeAdd:
		return "Add"
	case CaptureOutcomeUpdate:
		return "Update"
	case CaptureOutcomeAbsent:
		return "Remember absent"
	case CaptureOutcomePreserve:
		return "Preserve"
	case CaptureOutcomeBlocked:
		return "Blocked"
	case CaptureOutcomeStopManaging:
		return "Stop managing"
	default:
		return "No change"
	}
}

// CaptureTarget is one provider-defined target's read-only Capture preview:
// what a Capture run would do to it, and why.
type CaptureTarget struct {
	Category   string
	Inspection TargetInspection
	Policy     policy.EffectiveSetting
	Decision   CaptureDecision
	Outcome    CaptureOutcome
	// differs is whether Diff attributed a difference from saved state to
	// this target when it was inspected.
	differs bool
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
		items, err := s.inspectProviderCapture(ctx, provider)
		if err != nil {
			return CaptureInspection{}, err
		}
		if len(items) > 0 {
			categories[provider.ID()] = items
		}
	}
	return CaptureInspection{Categories: categories}, nil
}

func (s *Session) inspectProviderCapture(ctx context.Context, provider Provider) ([]CaptureTarget, error) {
	targets, err := provider.InspectTargets(ctx, s.profile)
	if err != nil {
		return nil, fmt.Errorf("inspect %s targets: %w", provider.ID(), err)
	}
	if len(targets) == 0 {
		return nil, nil
	}
	differs, err := targetDifferences(ctx, provider, s.profile, targets)
	if err != nil {
		return nil, fmt.Errorf("compare %s targets: %w", provider.ID(), err)
	}
	items := make([]CaptureTarget, 0, len(targets))
	for _, target := range targets {
		effective, decision, err := s.resolveCaptureTarget(ctx, provider.ID(), target)
		if err != nil {
			return nil, fmt.Errorf("resolve %s policy for %s: %w", provider.ID(), target.Key, err)
		}
		items = append(items, CaptureTarget{
			Category:   provider.ID(),
			Inspection: target,
			Policy:     effective,
			Decision:   decision,
			Outcome:    captureOutcomeFor(target, decision, differs(target.Key)),
			differs:    differs(target.Key),
		})
	}
	return items, nil
}

// recheckCapture re-reads the live inventory of ids after Capture staged
// its changes, recalculating outcomes against authority. It deliberately
// does not re-run Diff: staging may already have replaced saved copies in
// the profile directory. authority's decisions and saved-state comparison
// still apply to any target whose live value (presence and Fingerprint) is
// unchanged; anything new or changed is treated as differing, so a live
// change after the approval comparison still surfaces.
func (s *Session) recheckCapture(ctx context.Context, ids []string, authority CaptureInspection) (CaptureInspection, error) {
	categories := make(map[string][]CaptureTarget, len(ids))
	for _, id := range ids {
		provider, ok := ProviderByID(s.providers, id)
		if !ok {
			return CaptureInspection{}, fmt.Errorf("unknown category %s", id)
		}
		targets, err := provider.InspectTargets(ctx, s.profile)
		if err != nil {
			return CaptureInspection{}, fmt.Errorf("inspect %s targets: %w", id, err)
		}
		before := make(map[string]CaptureTarget, len(authority.Categories[id]))
		for _, target := range authority.Categories[id] {
			before[target.Inspection.Key] = target
		}
		items := make([]CaptureTarget, 0, len(targets))
		for _, target := range targets {
			effective, decision, err := s.resolveCaptureTarget(ctx, id, target)
			if err != nil {
				return CaptureInspection{}, fmt.Errorf("resolve %s policy for %s: %w", id, target.Key, err)
			}
			differs := true
			if prior, ok := before[target.Key]; ok && sameLiveValue(prior.Inspection, target) {
				differs = prior.differs
			}
			items = append(items, CaptureTarget{
				Category: id, Inspection: target, Policy: effective, Decision: decision,
				Outcome: captureOutcomeFor(target, decision, differs), differs: differs,
			})
		}
		if len(items) > 0 {
			categories[id] = items
		}
	}
	return CaptureInspection{Categories: categories}, nil
}

func sameLiveValue(a, b TargetInspection) bool {
	return a.Current == b.Current && a.Desired == b.Desired && a.CaptureEligible == b.CaptureEligible && a.Fingerprint == b.Fingerprint
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
// operation in their Restore plan to the policy target keys it acts on (a
// batched package install acts on several), so workflow can tell whether a
// difference is really a Restore candidate under the machine's effective
// Restore intent. ok=false means the operation cannot be attributed; ok=true
// with no keys means it only supports other operations (e.g. reloading
// Hyprland) and targets nothing.
type RestoreTargetResolver interface {
	RestoreOperationTargetKeys(op model.Operation) (keys []string, ok bool)
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

// InspectCaptureMany is InspectCapture for an explicit category choice, the
// same set CaptureMany would write, merged into one preview.
func (s *Session) InspectCaptureMany(ctx context.Context, ids []string) (CaptureInspection, error) {
	merged := CaptureInspection{Categories: map[string][]CaptureTarget{}}
	for _, id := range ids {
		inspection, err := s.InspectCapture(ctx, id)
		if err != nil {
			return CaptureInspection{}, err
		}
		for category, targets := range inspection.Categories {
			merged.Categories[category] = targets
		}
	}
	return merged, nil
}

// CaptureReviewChangedError reports that a fresh inspection no longer
// matches the review a user approved. Nothing was written; Fresh is the
// recalculated review that needs approving instead.
type CaptureReviewChangedError struct {
	Fresh   CaptureInspection
	Changes []string
}

func (e *CaptureReviewChangedError) Error() string {
	return fmt.Sprintf("Capture review changed since it was approved (%d %s)", len(e.Changes), map[bool]string{true: "difference", false: "differences"}[len(e.Changes) == 1])
}

// captureContexts turns an inspection into the Capture decisions each
// requested category runs with. A category with no inspected targets gets
// an empty context, so nothing outside the approved review is captured.
func (i CaptureInspection) captureContexts(machine string, ids []string) map[string]CaptureContext {
	contexts := make(map[string]CaptureContext, len(ids))
	for _, id := range ids {
		decisions := map[string]CaptureDecision{}
		for _, target := range i.Categories[id] {
			decisions[target.Inspection.Key] = target.Decision
		}
		contexts[id] = CaptureContext{Machine: machine, Targets: decisions}
	}
	return contexts
}

// ChangesFrom describes, in stable order, every target whose Capture
// outcome differs between i and fresh, or whose value changed while Capture
// would still write it (Update(A) → Update(B), via
// TargetInspection.Fingerprint). A target missing from one side counts as
// needing no action there, so only differences that change what Capture
// would write are material.
func (i CaptureInspection) ChangesFrom(fresh CaptureInspection) []string {
	type entry struct {
		label       string
		outcome     CaptureOutcome
		fingerprint string
	}
	index := func(inspection CaptureInspection) map[string]entry {
		entries := map[string]entry{}
		for category, targets := range inspection.Categories {
			for _, target := range targets {
				label := target.Inspection.Label
				if label == "" {
					label = target.Inspection.Key
				}
				entries[category+"\x00"+target.Inspection.Key] = entry{
					label:       strings.ToUpper(category[:1]) + category[1:] + " " + label,
					outcome:     target.Outcome,
					fingerprint: target.Inspection.Fingerprint,
				}
			}
		}
		return entries
	}
	before, after := index(i), index(fresh)
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	outcome := func(e entry, ok bool) CaptureOutcome {
		if !ok {
			return CaptureOutcomeNoop
		}
		return e.outcome
	}
	var changes []string
	for _, key := range ordered {
		was, hadBefore := before[key]
		now, hasNow := after[key]
		from, to := outcome(was, hadBefore), outcome(now, hasNow)
		label := now.label
		if !hasNow {
			label = was.label
		}
		switch {
		case from != to:
			changes = append(changes, fmt.Sprintf("%s: %s → %s", label, from.Label(), to.Label()))
		case writesValue(to) && was.fingerprint != now.fingerprint:
			changes = append(changes, fmt.Sprintf("%s: changed since the review (%s)", label, to.Label()))
		}
	}
	return changes
}

// writesValue reports whether an outcome records the target's current value.
func writesValue(outcome CaptureOutcome) bool {
	return outcome == CaptureOutcomeAdd || outcome == CaptureOutcomeUpdate
}
