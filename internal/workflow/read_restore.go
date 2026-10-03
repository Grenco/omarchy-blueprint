package workflow

import (
	"context"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/compatibility"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

// RestorePreview deliberately contains no execution contexts or providers.
type RestorePreview struct {
	Plan    model.RestorePlan
	Options policy.RestoreOptions
	Profile profile.Data
	Machine machine.Selection
}

func (s *Session) PreviewRestore(ctx context.Context, only string, options *policy.RestoreOptions) (RestorePreview, error) {
	return s.PreviewRestoreScope(ctx, onlyRestoreScope(only), options)
}

// PreviewRestoreScope previews the plan for an explicit RestoreScope.
func (s *Session) PreviewRestoreScope(ctx context.Context, scope RestoreScope, options *policy.RestoreOptions) (RestorePreview, error) {
	r, e := s.BeginRead(ctx)
	if e != nil {
		return RestorePreview{}, e
	}
	defer r.Close()
	return r.PreviewRestoreScope(ctx, scope, options)
}

func (r *ReadCycle) PreviewRestore(ctx context.Context, only string, options *policy.RestoreOptions) (RestorePreview, error) {
	return r.PreviewRestoreScope(ctx, onlyRestoreScope(only), options)
}

// PreviewRestoreScope previews the plan for an explicit RestoreScope inside
// this read cycle.
func (r *ReadCycle) PreviewRestoreScope(ctx context.Context, scope RestoreScope, options *policy.RestoreOptions) (RestorePreview, error) {
	ctx, stop := r.withContext(ctx)
	defer stop()
	if e := r.check(ctx); e != nil {
		return RestorePreview{}, e
	}
	resolved := policy.DefaultRestoreOptions()
	if r.machine.Machine != nil {
		resolved = r.machine.Machine.EffectiveRestoreDefaults()
	}
	if options != nil {
		resolved = cloneRestoreOptions(*options)
	}
	if e := policy.ValidateRestoreOptions(resolved); e != nil {
		return RestorePreview{}, e
	}
	selected, deferred, err := selectRestoreScope(r.providers, profile.CloneData(r.profile), scope)
	if err != nil {
		return RestorePreview{}, err
	}
	providers := make([]ReadRestoreProvider, 0, len(selected))
	for _, p := range selected {
		v, e := readRestoreProvider(p)
		if e != nil {
			return RestorePreview{}, e
		}
		providers = append(providers, v)
	}
	info, err := r.info.Get(ctx)
	if err != nil {
		return RestorePreview{}, err
	}
	plan := model.RestorePlan{ProfileVersion: r.profile.Manifest.Schema, OmarchyFrom: r.profile.Manifest.Omarchy.CapturedVersion, OmarchyTo: info.Version, Deferred: deferred}
	if resolved.Activation != "" {
		plan.ActivationMode = string(resolved.ActivationMode())
	}
	plan.Compatibility = model.CompatibilityReport{ProfileLastCapture: model.CompatibilityEnvironment{Known: r.profile.Manifest.Omarchy.CapturedVersion != "", OmarchyVersion: r.profile.Manifest.Omarchy.CapturedVersion, OmarchyChannel: r.profile.Manifest.Omarchy.Channel}, Target: model.CompatibilityEnvironment{Known: info.Version != "", OmarchyVersion: info.Version, OmarchyChannel: info.Channel}}
	ids := make([]string, 0, len(providers))
	targets := make(map[string]map[string]struct{}, len(providers))
	for _, p := range providers {
		inventory, e := p.InspectTargets(ctx, profile.CloneData(r.profile))
		if e != nil {
			return RestorePreview{}, fmt.Errorf("inspect %s targets: %w", p.ID(), e)
		}
		rc := RestoreContext{Machine: r.machine.Name, Options: cloneRestoreOptions(resolved), Targets: make(map[string]RestoreDecision, len(inventory))}
		targets[p.ID()] = make(map[string]struct{}, len(inventory))
		for _, target := range inventory {
			_, decision, e := r.resolveRestoreTarget(ctx, p.ID(), target)
			if e != nil {
				return RestorePreview{}, fmt.Errorf("resolve %s policy for %s: %w", p.ID(), target.Key, e)
			}
			rc.Targets[target.Key] = decision
			targets[p.ID()][target.Key] = struct{}{}
		}
		part, e := p.Plan(ctx, profile.CloneData(r.profile), info, rc)
		if e != nil {
			return RestorePreview{}, fmt.Errorf("plan %s restore: %w", p.ID(), e)
		}
		part = cloneReadFragment(part)
		ids = append(ids, p.ID())
		plan.Operations = append(plan.Operations, part.Operations...)
		plan.Skipped = append(plan.Skipped, part.Skipped...)
		plan.Requirements = append(plan.Requirements, part.Requirements...)
		plan.ActivationReview = append(plan.ActivationReview, part.ActivationReview...)
		plan.Compatibility.Categories = append(plan.Compatibility.Categories, part.Compatibility)
	}
	if r.finalize != nil {
		if e := r.finalize(ctx, profile.CloneData(r.profile), ids, &plan, resolved); e != nil {
			return RestorePreview{}, fmt.Errorf("finalize restore plan: %w", e)
		}
	} else if e := restore.ValidatePlan(plan); e != nil {
		return RestorePreview{}, e
	}
	plan.Operations = restore.AwaitingStepsLast(plan.Operations)
	if e := restore.ValidatePlan(plan); e != nil {
		return RestorePreview{}, e
	}
	plan.Compatibility = compatibility.NormalizeReport(plan.Compatibility)
	if e := compatibility.ValidateReport(plan.Compatibility, ids, targets, plan.Requirements); e != nil {
		return RestorePreview{}, fmt.Errorf("validate restore compatibility: %w", e)
	}
	if e := r.check(ctx); e != nil {
		return RestorePreview{}, e
	}
	return RestorePreview{Plan: cloneReadPlan(plan), Options: cloneRestoreOptions(resolved), Profile: profile.CloneData(r.profile), Machine: cloneMachine(r.machine)}, nil
}
