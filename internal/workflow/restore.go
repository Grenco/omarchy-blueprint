package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

type RestoreResult struct {
	Options      policy.RestoreOptions
	Plan         model.RestorePlan
	Execution    restore.Result
	Verification model.VerificationResult
	Journal      string
	Applied      bool
}

// PlanRestore returns the same validated plan used by preview and apply.
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
// provider alongside the exact resolved RestoreContext used to plan it, and
// the resolved RestoreOptions itself. A caller that must verify separately
// from ApplyRestore -- e.g. the CLI, which reports live per-operation
// progress ApplyRestore does not support, so it executes and verifies
// itself rather than calling ApplyRestore -- needs the providers/contexts
// to verify against the same effective Restore intent that planned the
// run, per the design's "Plan intent and verification intent must remain
// aligned" invariant; it needs the resolved options to know what a nil
// options argument actually resolved to (e.g. to decide how to render the
// plan).
func (s *Session) PlanRestoreWithContext(ctx context.Context, onlyProvider string, options *policy.RestoreOptions) (model.RestorePlan, []RestoreProvider, map[string]RestoreContext, policy.RestoreOptions, error) {
	return s.restorePlan(ctx, onlyProvider, options)
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
	if operation, ok := interactiveOperation(plan); ok {
		return result, fmt.Errorf("restore operation %s requires an interactive terminal and cannot run through this executor", operation.Resource)
	}
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

// ErrRestorePlanChanged means the recalculated plan differs from the one the
// user approved, so the approval does not cover it.
var ErrRestorePlanChanged = errors.New("restore plan changed after approval; inspect the new plan and approve again")

// UnmetRequirementsError refuses a plan whose requirements are unsatisfied.
type UnmetRequirementsError struct{ Requirements []model.Requirement }

func (e *UnmetRequirementsError) Error() string { return "not implemented" }

// ApplyApprovedRestore is not implemented yet.
func (s *Session) ApplyApprovedRestore(ctx context.Context, onlyProvider string, options *policy.RestoreOptions, approved model.RestorePlan, terminal bool) (RestoreResult, error) {
	return RestoreResult{}, errors.New("not implemented")
}

func interactiveOperation(plan model.RestorePlan) (model.Operation, bool) {
	for _, operation := range plan.Operations {
		if operation.Interactive {
			return operation, true
		}
	}
	return model.Operation{}, false
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
