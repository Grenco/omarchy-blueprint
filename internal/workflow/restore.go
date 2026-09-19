package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

type RestoreMode string

const (
	RestoreNormal RestoreMode = "normal"
	RestoreForced RestoreMode = "forced"
)

type Outcome string

const (
	OutcomeCreate  Outcome = "create"
	OutcomeModify  Outcome = "modify"
	OutcomeReplace Outcome = "replace"
	OutcomeDelete  Outcome = "delete"
	OutcomeSkip    Outcome = "skip"
	OutcomeNoop    Outcome = "noop"
)

type Consequence struct {
	Provider    string
	Resource    string
	OperationID string
	Normal      Outcome
	Forced      Outcome
	Difference  string
	Risk        model.Risk
	Diff        *inspection.DiffDocument
}

type RestoreComparison struct {
	Normal       model.RestorePlan
	Forced       model.RestorePlan
	Consequences []Consequence
}

type RestoreResult struct {
	Options      policy.RestoreOptions
	Plan         model.RestorePlan
	Execution    restore.Result
	Verification model.VerificationResult
	Journal      string
	Applied      bool
}

// CompareRestore is the temporary compatibility adapter the PR-2/3 TUI
// restore screen uses: a fixed Safe+Additive-versus-Force+Additive
// comparison, independent of the selected machine's persisted defaults or
// any one-run override. PR 7 removes this once the TUI is redesigned around
// the real two-axis RestoreOptions/policy Skip model.
func (s *Session) CompareRestore(ctx context.Context, onlyProvider string) (RestoreComparison, error) {
	safe := RestoreOptionsForMode(RestoreNormal)
	normal, _, _, _, err := s.restorePlan(ctx, onlyProvider, &safe)
	if err != nil {
		return RestoreComparison{}, err
	}
	forced := RestoreOptionsForMode(RestoreForced)
	forcedPlan, _, _, _, err := s.restorePlan(ctx, onlyProvider, &forced)
	if err != nil {
		return RestoreComparison{}, err
	}
	return RestoreComparison{Normal: normal, Forced: forcedPlan, Consequences: restoreConsequences(normal, forcedPlan)}, nil
}

// PlanRestore returns the same validated plan used by comparison and apply.
// options is this run's explicit two-axis restore intent; nil means the
// currently selected machine's persisted Restore defaults, falling back to
// the built-in Safe+Additive default when no machine is selected. An
// explicit options value is used verbatim for this run only -- it is never
// persisted as a new machine default.
func (s *Session) PlanRestore(ctx context.Context, onlyProvider string, options *policy.RestoreOptions) (model.RestorePlan, error) {
	plan, _, _, _, err := s.restorePlan(ctx, onlyProvider, options)
	return plan, err
}

// PlanRestoreWithContext is PlanRestore, but also returns each planned
// provider alongside the exact resolved RestoreContext used to plan it. A
// caller that must verify separately from ApplyRestore -- e.g. the CLI,
// which reports live per-operation progress ApplyRestore does not support,
// so it executes and verifies itself rather than calling ApplyRestore --
// needs these to verify against the same effective Restore intent that
// planned the run, per the design's "Plan intent and verification intent
// must remain aligned" invariant.
func (s *Session) PlanRestoreWithContext(ctx context.Context, onlyProvider string, options *policy.RestoreOptions) (model.RestorePlan, []RestoreProvider, map[string]RestoreContext, error) {
	plan, providers, contexts, _, err := s.restorePlan(ctx, onlyProvider, options)
	return plan, providers, contexts, err
}

// ApplyRestore always replans after approval, so the executor validates current
// filesystem preconditions rather than relying on a preview-time plan. See
// PlanRestore for how options resolves.
func (s *Session) ApplyRestore(ctx context.Context, onlyProvider string, options *policy.RestoreOptions) (RestoreResult, error) {
	plan, providers, contexts, resolved, err := s.restorePlan(ctx, onlyProvider, options)
	if err != nil {
		return RestoreResult{}, err
	}
	result := RestoreResult{Options: resolved, Plan: plan}
	if len(plan.Operations) == 0 {
		result.Verification, err = verifyRestoreProviders(ctx, s.profile, providers, contexts)
		return result, err
	}
	stateHome, err := s.deps.StateHome()
	if err != nil {
		return result, err
	}
	journal, err := restore.NewJournal(stateHome, s.deps.Now())
	if err != nil {
		return result, fmt.Errorf("create restore journal: %w", err)
	}
	defer journal.Close()
	result.Journal, result.Applied = journal.Path, true
	result.Execution, err = restore.Execute(ctx, s.deps.Runner, plan, journal, s.deps.Now, 5_000_000_000, nil)
	if err != nil {
		return result, err
	}
	result.Verification, err = verifyRestoreProviders(ctx, s.profile, providers, contexts)
	if err != nil {
		return result, err
	}
	_ = journal.Write(restore.Event{Time: s.deps.Now().UTC(), Type: "VERIFY_COMPLETED", Message: fmt.Sprintf("ok=%t", result.Verification.OK)})
	if len(result.Execution.Failed) > 0 {
		return result, fmt.Errorf("restore completed with %d failed operation(s)", len(result.Execution.Failed))
	}
	return result, nil
}

func (s *Session) restorePlan(ctx context.Context, only string, options *policy.RestoreOptions) (model.RestorePlan, []RestoreProvider, map[string]RestoreContext, policy.RestoreOptions, error) {
	if err := s.Reload(); err != nil {
		return model.RestorePlan{}, nil, nil, policy.RestoreOptions{}, err
	}
	resolved, err := s.resolveRestoreOptions(options)
	if err != nil {
		return model.RestorePlan{}, nil, nil, policy.RestoreOptions{}, err
	}
	selected := capturedProviders(s.providers, s.profile)
	if only != "" {
		provider, ok := ProviderByID(s.providers, only)
		if !ok {
			return model.RestorePlan{}, nil, nil, policy.RestoreOptions{}, fmt.Errorf("unknown category %s", only)
		}
		if !provider.Captured(s.profile) {
			return model.RestorePlan{}, nil, nil, policy.RestoreOptions{}, CaptureRequiredError(provider.ID())
		}
		selected = []Provider{provider}
	}
	providers := make([]RestoreProvider, 0, len(selected))
	for _, provider := range selected {
		restoreProvider, ok := provider.(RestoreProvider)
		if !ok {
			return model.RestorePlan{}, nil, nil, policy.RestoreOptions{}, fmt.Errorf("provider %s does not support restore", provider.ID())
		}
		providers = append(providers, restoreProvider)
	}
	info, err := omarchy.Detect(ctx, s.deps.Runner)
	if err != nil {
		return model.RestorePlan{}, nil, nil, policy.RestoreOptions{}, err
	}
	contexts := make(map[string]RestoreContext, len(providers))
	plan := model.RestorePlan{ProfileVersion: s.profile.Manifest.Schema, OmarchyFrom: s.profile.Manifest.Omarchy.CapturedVersion, OmarchyTo: info.Version}
	for _, provider := range providers {
		restoreCtx, err := s.resolveRestoreContext(ctx, provider, s.profile, s.machine.Name, resolved)
		if err != nil {
			return model.RestorePlan{}, nil, nil, policy.RestoreOptions{}, fmt.Errorf("inspect %s targets: %w", provider.ID(), err)
		}
		contexts[provider.ID()] = restoreCtx
		part, err := provider.Plan(ctx, s.profile, info, restoreCtx)
		if err != nil {
			return model.RestorePlan{}, nil, nil, policy.RestoreOptions{}, fmt.Errorf("plan %s restore: %w", provider.ID(), err)
		}
		plan.Operations, plan.Skipped = append(plan.Operations, part.Operations...), append(plan.Skipped, part.Skipped...)
	}
	if s.finalizeRestore != nil {
		if err := s.finalizeRestore(ctx, s.profile, selected, &plan, resolved); err != nil {
			return model.RestorePlan{}, nil, nil, policy.RestoreOptions{}, fmt.Errorf("finalize restore plan: %w", err)
		}
	} else if err := restore.ValidatePlan(plan); err != nil {
		return model.RestorePlan{}, nil, nil, policy.RestoreOptions{}, err
	}
	return plan, providers, contexts, resolved, nil
}

// resolveRestoreOptions returns this run's effective two-axis restore
// intent: an explicit one-run override when the caller supplied one
// (validated, never persisted), else the currently selected machine's
// persisted Restore defaults, else the built-in Safe+Additive default when
// no machine is selected.
func (s *Session) resolveRestoreOptions(options *policy.RestoreOptions) (policy.RestoreOptions, error) {
	if options != nil {
		if err := policy.ValidateRestoreOptions(*options); err != nil {
			return policy.RestoreOptions{}, err
		}
		return *options, nil
	}
	if s.machine.Machine != nil {
		return s.machine.Machine.EffectiveRestoreDefaults(), nil
	}
	return policy.DefaultRestoreOptions(), nil
}

// RestoreOptionsForMode translates the legacy two-value RestoreMode the
// PR-2/3 TUI restore screen still toggles into the two-axis
// policy.RestoreOptions CompareRestore/PlanRestore/ApplyRestore consume.
// Convergence is always Additive: Forced only ever meant Force+Additive, a
// fixed comparison baseline independent of any machine's persisted
// defaults. PR 7 removes this alongside CompareRestore and RestoreMode once
// the TUI is redesigned.
func RestoreOptionsForMode(mode RestoreMode) policy.RestoreOptions {
	conflicts := policy.ConflictSafe
	if mode == RestoreForced {
		conflicts = policy.ConflictForce
	}
	return policy.RestoreOptions{Conflicts: conflicts, Convergence: policy.ConvergenceAdditive}
}

// resolveRestoreContext inspects a provider's targets and resolves each
// one's real effective Restore policy (see resolveRestoreTarget) against
// the session's currently selected machine. Restore Skip -- whether from
// provider safety or policy -- is independent of options: Neither Force nor
// Exact can override it, so this never consults options for the decision
// itself, only records it on the returned context for providers to plan
// with.
func (s *Session) resolveRestoreContext(ctx context.Context, provider Provider, data profile.Data, machine string, options policy.RestoreOptions) (RestoreContext, error) {
	targets, err := provider.InspectTargets(ctx, data)
	if err != nil {
		return RestoreContext{}, err
	}
	decisions := make(map[string]RestoreDecision, len(targets))
	for _, target := range targets {
		_, decision, err := s.resolveRestoreTarget(ctx, provider.ID(), target)
		if err != nil {
			return RestoreContext{}, fmt.Errorf("resolve %s policy for %s: %w", provider.ID(), target.Key, err)
		}
		decisions[target.Key] = decision
	}
	return RestoreContext{Machine: machine, Options: options, Targets: decisions}, nil
}

// RestoreSkipReason formats a standardized, human-readable explanation for
// a policy-resolved Restore Skip decision, for a provider to attach
// verbatim to the model.Skipped entry it records for the target. It names
// the machine when the resolved policy came from a machine-scoped rule
// (target, category, or nearest ancestor override all set Source.Machine);
// a portable profile-scoped rule applies to every machine, so it is
// reported without a machine qualifier.
func RestoreSkipReason(setting policy.EffectiveSetting) string {
	if setting.Source.Machine != "" {
		return fmt.Sprintf("restore disabled for machine %q", setting.Source.Machine)
	}
	return "restore disabled"
}

// verifyRestoreProviders reuses the exact RestoreContext restorePlan built
// for each provider's Plan call, so verification never judges a run against
// a different effective Restore intent than the one that planned it.
func verifyRestoreProviders(ctx context.Context, data profile.Data, providers []RestoreProvider, contexts map[string]RestoreContext) (model.VerificationResult, error) {
	result := model.VerificationResult{OK: true}
	for _, provider := range providers {
		verification, err := provider.Verify(ctx, data, contexts[provider.ID()])
		if err != nil {
			return model.VerificationResult{}, fmt.Errorf("verify %s restore: %w", provider.ID(), err)
		}
		result.OK = result.OK && verification.OK
		result.Missing = append(result.Missing, verification.Missing...)
	}
	return result, nil
}

type restoreEntry struct {
	op   *model.Operation
	skip *model.Skipped
}

func restoreConsequences(normal, forced model.RestorePlan) []Consequence {
	index := func(plan model.RestorePlan) map[string]restoreEntry {
		items := map[string]restoreEntry{}
		for i := range plan.Operations {
			op := &plan.Operations[i]
			items[operationKey(*op)] = restoreEntry{op: op}
		}
		for i := range plan.Skipped {
			skip := &plan.Skipped[i]
			key := skip.Provider + "\x00" + skip.Resource
			if _, exists := items[key]; !exists {
				items[key] = restoreEntry{skip: skip}
			}
		}
		return items
	}
	a, b := index(normal), index(forced)
	keys := make([]string, 0, len(a)+len(b))
	for key := range a {
		keys = append(keys, key)
	}
	for key := range b {
		if _, ok := a[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	items := make([]Consequence, 0, len(keys))
	for _, key := range keys {
		n, f := a[key], b[key]
		consequence := Consequence{Normal: entryOutcome(n), Forced: entryOutcome(f)}
		base := n
		if base.op == nil && f.op != nil {
			base = f
		}
		if base.op != nil {
			consequence.Provider, consequence.Resource, consequence.OperationID, consequence.Risk = base.op.Provider, base.op.Resource, base.op.ID, base.op.Risk
			consequence.Diff = operationDiff(*base.op)
		} else if base.skip != nil {
			consequence.Provider, consequence.Resource = base.skip.Provider, base.skip.Resource
		}
		consequence.Difference = consequenceDescription(consequence.Normal, consequence.Forced)
		items = append(items, consequence)
	}
	return items
}

func operationKey(op model.Operation) string { return op.Provider + "\x00" + op.Resource }
func entryOutcome(entry restoreEntry) Outcome {
	if entry.op == nil {
		if entry.skip != nil {
			return OutcomeSkip
		}
		return OutcomeNoop
	}
	return operationOutcome(*entry.op)
}
func operationOutcome(op model.Operation) Outcome {
	if op.Delete != nil {
		return OutcomeDelete
	}
	if op.File != nil {
		if op.File.ReplaceExisting {
			return OutcomeReplace
		}
		if op.File.ExpectedMissing {
			return OutcomeCreate
		}
		return OutcomeModify
	}
	if op.Symlink != nil {
		if op.Symlink.ReplaceExisting {
			return OutcomeReplace
		}
		if op.Symlink.ExpectedMissing {
			return OutcomeCreate
		}
		return OutcomeModify
	}
	if op.Directory != nil {
		return OutcomeCreate
	}
	if op.Copy != nil || op.GitPatch != nil || len(op.Command) > 0 {
		return OutcomeModify
	}
	return OutcomeNoop
}
func consequenceDescription(normal, forced Outcome) string {
	if normal == forced {
		return "same effect in normal and forced modes"
	}
	return fmt.Sprintf("normal: %s; forced: %s", normal, forced)
}

func operationDiff(op model.Operation) *inspection.DiffDocument {
	if op.File == nil {
		return nil
	}
	newBytes, ok := fileContent(*op.File)
	if !ok {
		return &inspection.DiffDocument{Kind: inspection.DiffUnavailable, Metadata: []inspection.DiffFact{{Key: "reason", Value: "restore content is unavailable"}}}
	}
	oldBytes, ok := boundedFile(op.File.Destination)
	if !ok {
		return &inspection.DiffDocument{Kind: inspection.DiffUnavailable, OldLabel: op.File.Destination, NewLabel: op.Resource, Metadata: []inspection.DiffFact{{Key: "reason", Value: "existing file is unavailable"}}}
	}
	doc := inspection.BuildTextDiff(op.File.Destination, oldBytes, op.Resource, newBytes)
	return &doc
}
func fileContent(file model.FileWrite) ([]byte, bool) {
	if file.Content != nil {
		return file.Content, true
	}
	return boundedFile(file.Source)
}
func boundedFile(path string) ([]byte, bool) {
	if path == "" {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, inspection.MaxPreviewBytes+1))
	return data, err == nil
}
