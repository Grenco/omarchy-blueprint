package autosync

import (
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// Reason is a stable, machine-readable code for why an operation may or may
// not run unattended. Plain-English copy belongs to the TUI/CLI.
type Reason string

const (
	ReasonEligible Reason = "eligible"
	// Grant state (run-time blockers).
	ReasonOff                Reason = "auto-sync-off"
	ReasonPaused             Reason = "auto-sync-paused"
	ReasonSuspended          Reason = "suspended-for-safety"
	ReasonNotifyOnly         Reason = "notify-only"
	ReasonIdentityMismatch   Reason = "identity-mismatch"
	ReasonCapabilityVersion  Reason = "capability-version-changed"
	ReasonApprovalOutOfDate  Reason = "permissions-not-confirmed"
	ReasonUnresolvedProvider Reason = "provider-mismatch"
	// Descriptor and hard denials (T3 forms).
	ReasonUndescribed         Reason = "undescribed-operation"
	ReasonInteractionRequired Reason = "needs-a-person"
	ReasonUnpinnedSource      Reason = "unverified-source"
	ReasonUnprovenRemoval     Reason = "unproven-removal"
	ReasonUnregistered        Reason = "unregistered-operation"
	ReasonUnverifiable        Reason = "result-cannot-be-verified"
	ReasonEvidenceRejected    Reason = "evidence-changed-the-operation"
	// Registry.
	ReasonNotAvailableYet   Reason = "not-available-yet"
	ReasonConditionUnproven Reason = "condition-unproven"
	ReasonShapeMismatch     Reason = "unsupported-operation-shape"
	// Authority.
	ReasonAbovePreset       Reason = "above-preset"
	ReasonOutsideGrant      Reason = "outside-grant"
	ReasonRestoreWidened    Reason = "restore-widened-beyond-confirmed"
	ReasonTargetDenied      Reason = "target-denied"
	ReasonFamilyDenied      Reason = "operation-type-denied"
	ReasonRestoreSkip       Reason = "restore-skip"
	ReasonRunCodeRequired   Reason = "run-code-not-authorised"
	ReasonPrivilegedNeeded  Reason = "privileged-install-not-authorised"
	ReasonUnattributed      Reason = "needs-category-wide-permission"
	ReasonMayPrompt         Reason = "may-prompt"
	ReasonDependencyMissing Reason = "dependency-missing"
	ReasonDependencyCycle   Reason = "dependency-cycle"
	ReasonDependencyBlocked Reason = "dependency-needs-review"
)

// Evidence proves conditions and may refine a descriptor with positive
// evidence (for example, establishing that a configuration file is inert).
// It must never weaken a descriptor without proof; NoEvidence proves nothing.
type Evidence interface {
	Refine(op model.Operation, described model.Effect) (model.Effect, map[Condition]bool)
}

// NoEvidence proves no condition and refines nothing.
type NoEvidence struct{}

func (NoEvidence) Refine(_ model.Operation, described model.Effect) (model.Effect, map[Condition]bool) {
	return described, nil
}

// OperationInput is one operation of a fresh executable Restore plan and
// what is known about its targets.
type OperationInput struct {
	Operation model.Operation
	// Category is the provider category that planned it.
	Category string
	// Targets are the provider-owned target keys the operation changes; nil
	// when the operation is a category-level step (reload, restart).
	Targets []string
	// RestoreApply reports, per target, whether current effective Restore
	// policy is Apply. Skip always vetoes.
	RestoreApply map[string]bool
}

// Decision is the evaluator's verdict for one operation.
type Decision struct {
	OperationID string   `json:"operation_id"`
	Category    string   `json:"category"`
	Family      string   `json:"family,omitempty"`
	Targets     []string `json:"targets,omitempty"`
	Eligible    bool     `json:"eligible"`
	Reason      Reason   `json:"reason"`
	// Class and Increment describe the lowest registry entry that would
	// apply, for "available now" versus "not available yet" copy.
	Class     Class  `json:"class"`
	Increment string `json:"increment,omitempty"`
}

// Evaluation is the whole-plan verdict. Eligible requires every operation
// to be eligible and at least one operation; it is advisory until rechecked
// at the locked execution boundary.
type Evaluation struct {
	Eligible  bool       `json:"eligible"`
	Decisions []Decision `json:"decisions"`
}

// Evaluate classifies every operation of a fresh plan against the grant.
// current is this computer's identity now.
func Evaluate(g Grant, current Identity, registry Registry, evidence Evidence, ops []OperationInput) Evaluation {
	if evidence == nil {
		evidence = NoEvidence{}
	}
	gate := grantGate(g, current, registry)
	byID := map[string]int{}
	decisions := make([]Decision, len(ops))
	for i, in := range ops {
		byID[in.Operation.ID] = i
		if gate != "" {
			decisions[i] = Decision{OperationID: in.Operation.ID, Category: in.Category, Targets: in.Targets, Reason: gate}
			if in.Operation.Effect != nil {
				decisions[i].Family = in.Operation.Effect.Family
			}
			continue
		}
		decisions[i] = evaluateOne(g, registry, evidence, in)
	}
	propagateDependencies(ops, byID, decisions)
	result := Evaluation{Decisions: decisions, Eligible: len(decisions) > 0}
	for _, d := range decisions {
		result.Eligible = result.Eligible && d.Eligible
	}
	return result
}

func grantGate(g Grant, current Identity, registry Registry) Reason {
	switch {
	case g.Setting == SettingOff:
		return ReasonOff
	case g.Setting == SettingPaused:
		return ReasonPaused
	case g.Suspended != nil:
		return ReasonSuspended
	case g.CheckIdentity(current) != nil:
		return ReasonIdentityMismatch
	case g.CapabilityVersion != registry.Version:
		return ReasonCapabilityVersion
	case g.Approved == "" || g.Approved != g.AuthorityDigest():
		return ReasonApprovalOutOfDate
	case g.Preset == PresetNotifyOnly:
		return ReasonNotifyOnly
	}
	return ""
}

func evaluateOne(g Grant, registry Registry, evidence Evidence, in OperationInput) Decision {
	op := in.Operation
	d := Decision{OperationID: op.ID, Category: in.Category, Targets: in.Targets}
	if op.Effect == nil || !op.Effect.Complete() {
		d.Reason = ReasonUndescribed
		return d
	}
	described := *op.Effect
	d.Family = described.Family
	if categoryOf(described.Family) != in.Category || op.Provider != in.Category {
		d.Reason = ReasonUnresolvedProvider
		return d
	}
	if reason := hardDenial(described); reason != "" {
		d.Reason = reason
		return d
	}
	entries := registry.entries(described.Family)
	if len(entries) == 0 {
		d.Reason = ReasonUnregistered
		return d
	}
	effect, proven := evidence.Refine(op, described)
	if reason := checkRefinement(described, effect, proven); reason != "" {
		d.Reason = reason
		return d
	}
	// Hard denials again, on the final effect.
	if reason := hardDenial(effect); reason != "" {
		d.Reason = reason
		return d
	}
	if !effect.Verifiable && !proven[CondOutcomeVerified] {
		d.Reason = ReasonUnverifiable
		return d
	}
	// Report the lowest entry so copy can say what is planned.
	d.Class, d.Increment = entries[0].Class, entries[0].Increment
	reason := ReasonNotAvailableYet
	for _, entry := range entries {
		if !entry.Delivered {
			continue
		}
		if !matches(entry, effect) {
			reason = worst(reason, ReasonShapeMismatch)
			continue
		}
		if !allProven(entry.Conditions, proven) {
			reason = worst(reason, ReasonConditionUnproven)
			continue
		}
		if effect.Interaction == model.InteractionPossible && !proven[CondNoninteractive] {
			reason = worst(reason, ReasonMayPrompt)
			continue
		}
		d.Class, d.Increment = entry.Class, entry.Increment
		if r := authorise(g, entry, effect, in); r != ReasonEligible {
			reason = worst(reason, r)
			continue
		}
		d.Eligible, d.Reason = true, ReasonEligible
		return d
	}
	d.Reason = reason
	return d
}

// hardDenial reports forms the audit classifies as never unattended.
func hardDenial(e model.Effect) Reason {
	switch {
	case e.Interaction == model.InteractionRequired:
		return ReasonInteractionRequired
	case e.Source == model.SourceUnpinned:
		return ReasonUnpinnedSource
	case e.Change == model.EffectRemove && e.Recovery == model.RecoveryNone:
		return ReasonUnprovenRemoval
	}
	return ""
}

var payloadRank = map[model.EffectPayload]int{model.PayloadNone: 0, model.PayloadData: 1, model.PayloadConfig: 2, model.PayloadExecLater: 3, model.PayloadExecNow: 4}

// checkRefinement lets evidence do exactly one thing: lower the payload
// (for example, prove a file cannot run code), and only with positive
// evidence. Every other dimension must be unchanged.
func checkRefinement(described, refined model.Effect, proven map[Condition]bool) Reason {
	if !refined.Complete() {
		return ReasonUndescribed
	}
	same := refined
	same.Payload = described.Payload
	if same != described {
		return ReasonEvidenceRejected
	}
	if refined.Payload != described.Payload && (payloadRank[refined.Payload] > payloadRank[described.Payload] || !proven[CondPositiveEvidence]) {
		return ReasonEvidenceRejected
	}
	return ""
}

// worst keeps the most specific explanation across entries: a reason from
// an entry that matched and was proven (an authority reason) says more than
// an unproven condition, which says more than a shape mismatch or an entry
// that is not delivered yet.
func worst(current, next Reason) Reason {
	if rank(next) > rank(current) {
		return next
	}
	return current
}

func rank(r Reason) int {
	switch r {
	case ReasonNotAvailableYet:
		return 0
	case ReasonShapeMismatch:
		return 1
	case ReasonConditionUnproven:
		return 2
	case ReasonMayPrompt:
		return 3
	}
	return 4
}

func matches(entry Entry, e model.Effect) bool {
	okActivation := false
	for _, a := range entry.Activations {
		okActivation = okActivation || a == e.Activation
	}
	if !okActivation {
		return false
	}
	okChange := len(entry.Changes) == 0
	for _, c := range entry.Changes {
		okChange = okChange || c == e.Change
	}
	okPayload := len(entry.Payloads) == 0
	for _, p := range entry.Payloads {
		okPayload = okPayload || p == e.Payload
	}
	okSource := len(entry.Sources) == 0
	for _, s := range entry.Sources {
		okSource = okSource || s == e.Source
	}
	return okChange && okPayload && okSource
}

func allProven(conditions []Condition, proven map[Condition]bool) bool {
	for _, c := range conditions {
		if !proven[c] {
			return false
		}
	}
	return true
}

func codeRunning(e model.Effect) bool {
	return e.Payload == model.PayloadExecLater || e.Payload == model.PayloadExecNow
}

func authorise(g Grant, entry Entry, e model.Effect, in OperationInput) Reason {
	if entry.Class > g.ceiling(in.Category) {
		return ReasonAbovePreset
	}
	scope, ok := g.Categories[in.Category]
	if !ok {
		return ReasonOutsideGrant
	}
	if contains(scope.DenyFamilies, e.Family) || (len(scope.AllowFamilies) > 0 && !contains(scope.AllowFamilies, e.Family)) {
		return ReasonFamilyDenied
	}
	if len(in.Targets) == 0 && !scope.AllTargets {
		return ReasonUnattributed
	}
	for _, target := range in.Targets {
		if contains(scope.DenyTargets, target) {
			return ReasonTargetDenied
		}
		if !in.RestoreApply[target] {
			return ReasonRestoreSkip
		}
		if !scope.AllTargets && !contains(scope.Targets, target) {
			if g.RestoreLink == LinkLinked {
				return ReasonRestoreWidened
			}
			return ReasonOutsideGrant
		}
	}
	// Code-running operations need the run-code authorisation of every
	// category the entry names, and of the operation's own category.
	required := append([]string(nil), entry.RunCode...)
	if codeRunning(e) && !contains(required, in.Category) {
		required = append(required, in.Category)
	}
	for _, category := range required {
		run, ok := g.RunCode[category]
		if !ok || (category == in.Category && len(run.Families) > 0 && !contains(run.Families, e.Family)) {
			return ReasonRunCodeRequired
		}
	}
	if (entry.Privileged || e.Privilege == model.PrivilegeRoot) && !(entry.Privileged && g.PrivilegedInstall) {
		return ReasonPrivilegedNeeded
	}
	return ReasonEligible
}

// propagateDependencies makes an operation ineligible when any dependency is
// missing, cyclic or itself not eligible, so a bundle cannot hide a step.
func propagateDependencies(ops []OperationInput, byID map[string]int, decisions []Decision) {
	const (
		unvisited = iota
		visiting
		done
	)
	state := make([]int, len(ops))
	var visit func(i int) bool
	visit = func(i int) bool {
		switch state[i] {
		case done:
			return decisions[i].Eligible
		case visiting:
			return false
		}
		state[i] = visiting
		for _, dep := range ops[i].Operation.DependsOn {
			j, ok := byID[dep]
			if !ok {
				if decisions[i].Eligible {
					decisions[i].Eligible, decisions[i].Reason = false, ReasonDependencyMissing
				}
				continue
			}
			if state[j] == visiting {
				decisions[i].Eligible, decisions[i].Reason = false, ReasonDependencyCycle
				decisions[j].Eligible, decisions[j].Reason = false, ReasonDependencyCycle
				continue
			}
			if !visit(j) && decisions[i].Eligible {
				decisions[i].Eligible, decisions[i].Reason = false, ReasonDependencyBlocked
			}
		}
		state[i] = done
		return decisions[i].Eligible
	}
	for i := range ops {
		visit(i)
	}
}

// Availability lists, for one preset, which registry families could run
// now and which are planned but not available yet.
type Availability struct {
	Preset          Preset   `json:"preset"`
	AvailableNow    []string `json:"available_now"`
	NotAvailableYet []string `json:"not_available_yet"`
}

func PresetAvailability(registry Registry, preset Preset) Availability {
	a := Availability{Preset: preset}
	now, later := map[string]bool{}, map[string]bool{}
	for _, e := range registry.Entries {
		if e.Class == ClassNone || e.Class > preset.Ceiling() {
			continue
		}
		if e.Delivered {
			now[e.Family] = true
		} else {
			later[e.Family] = true
		}
	}
	for family := range now {
		a.AvailableNow = append(a.AvailableNow, family)
		delete(later, family)
	}
	for family := range later {
		a.NotAvailableYet = append(a.NotAvailableYet, family)
	}
	sort.Strings(a.AvailableNow)
	sort.Strings(a.NotAvailableYet)
	return a
}

// PresetScope builds a preset's category scope covering every current and
// future target of the chosen categories.
func PresetScope(categories []string) map[string]CategoryScope {
	out := map[string]CategoryScope{}
	for _, c := range categories {
		out[c] = CategoryScope{AllTargets: true}
	}
	return out
}

// ScopeFromRestore copies current Restore Apply targets into a frozen
// scope (Copy once), or records the ceiling confirmed under Keep linked.
func ScopeFromRestore(apply map[string][]string) map[string]CategoryScope {
	out := map[string]CategoryScope{}
	for category, targets := range apply {
		out[category] = CategoryScope{Targets: sortedCopy(targets)}
	}
	return out
}
