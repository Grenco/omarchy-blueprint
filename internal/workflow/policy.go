package workflow

import (
	"context"
	"fmt"
	"strings"

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

// targetValidator is implemented by a category's stateProvider when it has a
// structured target key shape (e.g. "official:<name>" for packages, a raw
// path for hooks/config). It validates and canonicalizes a target string
// without requiring the target to currently be installed/present -- a
// desired-absent tombstone or a not-yet-existing target must validate too.
// A category without a validator accepts any non-empty target string as-is.
type targetValidator interface {
	ValidateTarget(target string) (string, error)
}

// validateTarget canonicalizes target against category's validator, if the
// category has one and target is non-empty (an empty target means a
// category-level rule, which has no target shape to validate).
func validateTarget(provider Provider, target string) (string, error) {
	if target == "" {
		return target, nil
	}
	validator, ok := provider.(targetValidator)
	if !ok {
		return target, nil
	}
	return validator.ValidateTarget(target)
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
	provider, ok := ProviderByID(s.providers, category)
	if !ok {
		return fmt.Errorf("workflow: unknown policy category %q", category)
	}
	target, err := validateTarget(provider, target)
	if err != nil {
		return err
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
// does not exist is not an error. An unknown category is treated the same
// way: there is nothing to canonicalize target against, and removing a rule
// from a category that could never have recorded one is already a no-op.
func (s *Session) ClearPolicy(scope PolicyScope, axis policy.Axis, category, target string) error {
	if err := policy.ValidateAxis(axis); err != nil {
		return err
	}
	if provider, ok := ProviderByID(s.providers, category); ok {
		canonical, err := validateTarget(provider, target)
		if err != nil {
			return err
		}
		target = canonical
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
//
// A provider that also implements captureTransaction (the same optional
// commit/finalize/rollback hooks CaptureMany uses) is treated as staging its
// artifact removal rather than performing it immediately: the artifact is
// only permanently deleted (FinalizeCapture) after this profile save
// succeeds, and the staged removal is undone (RollbackCapture) if it does
// not, so a failed save can never leave the profile referencing an artifact
// that has already been destroyed.
func (s *Session) StopManaging(ctx context.Context, category, target string) error {
	provider, ok := ProviderByID(s.providers, category)
	if !ok {
		return fmt.Errorf("workflow: unknown provider %q", category)
	}
	target, err := validateTarget(provider, target)
	if err != nil {
		return err
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
	transaction, hasTransaction := provider.(captureTransaction)
	if err := profile.Save(s.opts.ProfileDir, next); err != nil {
		if hasTransaction {
			_ = transaction.RollbackCapture()
		}
		return fmt.Errorf("save profile: %w", err)
	}
	if hasTransaction {
		if err := transaction.CommitCapture(); err != nil {
			return fmt.Errorf("commit stop managing: %w", err)
		}
		if err := transaction.FinalizeCapture(); err != nil {
			s.profile = next
			return fmt.Errorf("finalize stop managing: %w", err)
		}
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

// packageExcluded reports whether ref has an explicit profile-defaults
// Capture Disabled rule, the signal SetPackageExcluded records at the
// moment of exclusion. This is a read of the current policy-based state,
// not the retired Packages.Excluded list.
func (s *Session) packageExcluded(ref string) bool {
	for _, rule := range s.profile.Policy.Capture {
		if rule.Category == "packages" && rule.Target == ref && rule.Setting == policy.SettingDisabled {
			return true
		}
	}
	return false
}

// SetPackageExcluded records or clears a package/tool's exclusion. This is
// the sole mechanism the exclude/include CLI verbs and the TUI's package
// toggle use; there is no longer a separate Packages.Excluded list.
//
// Excluding strips ref from desired-present state (Official/AUR/Mise)
// immediately -- the exclusion has no desired state at all, matching the
// design's "Capture: Preserve/disabled, Restore: Skip/disabled, Desired
// state: unmanaged" -- and records Capture Disabled + Restore Disabled
// policy for it, so a later enabled Capture does not silently re-adopt it
// (Merge's disabled row preserves whatever was desired; stripping first is
// what makes that "nothing"). Including clears those policy rules; it does
// not attempt to restore any prior desired state, since excluding left
// none to restore -- a later Capture will naturally rediscover the ref if
// it is still installed.
func (s *Session) SetPackageExcluded(ref string, excluded bool) error {
	provider, ok := ProviderByID(s.providers, "packages")
	if !ok {
		return fmt.Errorf("workflow: packages provider is unavailable")
	}
	canonical, err := validateTarget(provider, ref)
	if err != nil {
		return err
	}
	if !excluded {
		if err := s.ClearPolicy(PolicyScope{}, policy.AxisCapture, "packages", canonical); err != nil {
			return err
		}
		return s.ClearPolicy(PolicyScope{}, policy.AxisRestore, "packages", canonical)
	}
	kind, name, _ := axisTargetParts(canonical)
	data := s.profile
	switch kind {
	case "official":
		data.Packages.Official = withoutString(data.Packages.Official, name)
	case "aur":
		data.Packages.AUR = withoutString(data.Packages.AUR, name)
	case "mise":
		data.Packages.Mise = withoutMiseTool(data.Packages.Mise, name)
	}
	if err := profile.Save(s.opts.ProfileDir, data); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	s.profile = data
	if err := s.SetPolicy(PolicyScope{}, policy.AxisCapture, "packages", canonical, policy.SettingDisabled); err != nil {
		return err
	}
	return s.SetPolicy(PolicyScope{}, policy.AxisRestore, "packages", canonical, policy.SettingDisabled)
}

func axisTargetParts(canonical string) (kind, name string, ok bool) {
	kind, name, ok = strings.Cut(canonical, ":")
	return kind, name, ok
}

// withoutString returns a new slice with value removed, never mutating
// values' own backing array (which may be shared with the session's stored
// profile).
func withoutString(values []string, value string) []string {
	out := make([]string, 0, len(values))
	for _, current := range values {
		if current != value {
			out = append(out, current)
		}
	}
	return out
}

// withoutMiseTool returns a new map with id removed, never mutating tools.
func withoutMiseTool(tools profile.MiseTools, id string) profile.MiseTools {
	out := make(profile.MiseTools, len(tools))
	for existing, tool := range tools {
		if existing != id {
			out[existing] = tool
		}
	}
	return out
}
