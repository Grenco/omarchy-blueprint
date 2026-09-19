package workflow

import (
	"context"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// PolicyScope identifies which policy scope a caller is viewing or editing.
// An empty Machine means the portable profile-defaults scope; this is passed
// through to policy.ResolveRequest.Machine unchanged so Resolve's
// scope-relative Explicit reporting stays correct for both scopes.
type PolicyScope struct {
	Machine string
}

// EffectivePolicy resolves both Capture and Restore policy for one target as
// viewed from scope. scope is explicit rather than the session's currently
// selected machine: the TUI must be able to inspect Profile defaults and a
// named machine independently, and an implicit current-machine resolution
// would tie every read to session state instead of the scope the caller is
// actually viewing.
func (s *Session) EffectivePolicy(ctx context.Context, scope PolicyScope, category string, target TargetInspection) (policy.Effective, error) {
	_ = ctx
	machineRules, err := s.scopedMachineRules(scope)
	if err != nil {
		return policy.Effective{}, err
	}
	capture, err := policy.Resolve(policy.ResolveRequest{
		Axis: policy.AxisCapture, Machine: scope.Machine, Category: category, Target: target.Key, Ancestors: target.Ancestors,
		MachineRules: machineRules, ProfileRules: s.profile.Policy, DefaultEnabled: true,
	})
	if err != nil {
		return policy.Effective{}, err
	}
	restore, err := policy.Resolve(policy.ResolveRequest{
		Axis: policy.AxisRestore, Machine: scope.Machine, Category: category, Target: target.Key, Ancestors: target.Ancestors,
		MachineRules: machineRules, ProfileRules: s.profile.Policy, DefaultEnabled: true,
	})
	if err != nil {
		return policy.Effective{}, err
	}
	return policy.Effective{Capture: capture, Restore: restore}, nil
}

// resolveCaptureTarget resolves one target's effective Capture policy as
// viewed from the session's currently selected machine (Capture always
// happens on the machine running Blueprint, unlike EffectivePolicy's
// general TUI-facing scope), and derives the final CaptureDecision safety
// gates policy: a target the provider itself reports ineligible for Capture
// resolves to disabled regardless of what policy says, since Capabilities
// are descriptive and provider safety checks remain authoritative. It
// returns the raw resolved policy.EffectiveSetting alongside the decision so
// callers (InspectCapture) can show why, without a second resolution.
func (s *Session) resolveCaptureTarget(ctx context.Context, category string, target TargetInspection) (policy.EffectiveSetting, CaptureDecision, error) {
	effective, err := s.EffectivePolicy(ctx, PolicyScope{Machine: s.machine.Name}, category, target)
	if err != nil {
		return policy.EffectiveSetting{}, CaptureDecision{}, err
	}
	if !target.CaptureEligible {
		return effective.Capture, CaptureDecision{Capture: false, Resolved: true}, nil
	}
	return effective.Capture, CaptureDecision{Capture: effective.Capture.Enabled, Resolved: true}, nil
}

// SetPolicy records an explicit Capture or Restore override for one category
// or target at scope. An empty target records a category-level rule.
func (s *Session) SetPolicy(scope PolicyScope, axis policy.Axis, category, target string, setting policy.Setting) error {
	if err := policy.ValidateAxis(axis); err != nil {
		return err
	}
	if err := policy.ValidateSetting(setting); err != nil {
		return err
	}
	if _, ok := ProviderByID(s.providers, category); !ok {
		return fmt.Errorf("workflow: unknown policy category %q", category)
	}
	rule := policy.Rule{Category: category, Target: target, Setting: setting}
	if scope.Machine == "" {
		s.profile.Policy = upsertPolicyRule(s.profile.Policy, axis, rule)
		return s.saveMachines()
	}
	m, err := s.machineByName(scope.Machine)
	if err != nil {
		return err
	}
	m.Policy = upsertPolicyRule(m.Policy, axis, rule)
	return s.saveMachines()
}

// ClearPolicy removes an explicit override for one category or target at
// scope, reverting it to inherited/default resolution. Clearing a rule that
// does not exist is not an error.
func (s *Session) ClearPolicy(scope PolicyScope, axis policy.Axis, category, target string) error {
	if err := policy.ValidateAxis(axis); err != nil {
		return err
	}
	if scope.Machine == "" {
		s.profile.Policy = removePolicyRule(s.profile.Policy, axis, category, target)
		return s.saveMachines()
	}
	m, err := s.machineByName(scope.Machine)
	if err != nil {
		return err
	}
	m.Policy = removePolicyRule(m.Policy, axis, category, target)
	return s.saveMachines()
}

// stopManagingActions is implemented by every provider's stateProvider,
// including the ones that reject Stop Managing outright: rejection is
// expressed as an error from StopManaging itself (with a category-specific
// explanation), not by a provider omitting the method, so every category
// gets a clear, specific message rather than a generic "unsupported."
type stopManagingActions interface {
	StopManaging(ctx context.Context, data profile.Data, target string) (profile.Data, error)
}

// StopManaging permanently forgets one target: its desired present/absent
// state, any sole captured artifact, and its target-specific profile and
// machine policy overrides. Unlike ClearPolicy, which reverts a target to
// inherited resolution, Stop Managing removes the target from Blueprint's
// tracking entirely. Support and exact semantics are category-specific; see
// each stateProvider's StopManaging for what "remove" means for it.
func (s *Session) StopManaging(ctx context.Context, category, target string) error {
	provider, ok := ProviderByID(s.providers, category)
	if !ok {
		return fmt.Errorf("workflow: unknown provider %q", category)
	}
	actions, ok := provider.(stopManagingActions)
	if !ok {
		return fmt.Errorf("%s does not support Stop Managing", category)
	}
	next, err := actions.StopManaging(ctx, s.profile, target)
	if err != nil {
		return err
	}
	next.Policy = removePolicyRule(removePolicyRule(next.Policy, policy.AxisCapture, category, target), policy.AxisRestore, category, target)
	for i := range next.Machines.Items {
		m := &next.Machines.Items[i]
		m.Policy = removePolicyRule(removePolicyRule(m.Policy, policy.AxisCapture, category, target), policy.AxisRestore, category, target)
	}
	if err := profile.Save(s.opts.ProfileDir, next); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	s.profile = next
	return nil
}

// SetMachineRestoreDefaults sets machine's persisted Restore conflict and
// convergence defaults. Both axes must hold an accepted value; use
// EffectivePolicy/profile.Machine.EffectiveRestoreDefaults to read the
// Safe/Additive default when a machine has no override.
func (s *Session) SetMachineRestoreDefaults(machineName string, options policy.RestoreOptions) error {
	if err := policy.ValidateRestoreOptions(options); err != nil {
		return err
	}
	m, err := s.machineByName(machineName)
	if err != nil {
		return err
	}
	m.RestoreConflicts = options.Conflicts
	m.RestoreConvergence = options.Convergence
	return s.saveMachines()
}

// scopedMachineRules returns the machine rules Resolve should consult for
// scope: none at profile-defaults scope (Resolve itself rejects a Machine
// override supplied there), or the named machine's own policy.
func (s *Session) scopedMachineRules(scope PolicyScope) (policy.Rules, error) {
	if scope.Machine == "" {
		return policy.Rules{}, nil
	}
	m, err := s.machineByName(scope.Machine)
	if err != nil {
		return policy.Rules{}, err
	}
	return m.Policy, nil
}

func upsertPolicyRule(rules policy.Rules, axis policy.Axis, rule policy.Rule) policy.Rules {
	list := axisRuleList(&rules, axis)
	for i := range *list {
		if (*list)[i].Category == rule.Category && (*list)[i].Target == rule.Target {
			(*list)[i].Setting = rule.Setting
			return rules
		}
	}
	*list = append(*list, rule)
	return rules
}

func removePolicyRule(rules policy.Rules, axis policy.Axis, category, target string) policy.Rules {
	list := axisRuleList(&rules, axis)
	filtered := (*list)[:0]
	for _, rule := range *list {
		if rule.Category == category && rule.Target == target {
			continue
		}
		filtered = append(filtered, rule)
	}
	*list = filtered
	return rules
}

func axisRuleList(rules *policy.Rules, axis policy.Axis) *[]policy.Rule {
	if axis == policy.AxisRestore {
		return &rules.Restore
	}
	return &rules.Capture
}
