package workflow

import (
	"context"
	"errors"
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
	machineRules, err = remapLegacyPreinstallPolicyRules(machineRules, category, target.Key)
	if err != nil {
		return policy.Effective{}, err
	}
	profileRules, err := remapLegacyPreinstallPolicyRules(s.profile.Policy, category, target.Key)
	if err != nil {
		return policy.Effective{}, err
	}
	capture, err := policy.Resolve(policy.ResolveRequest{
		Axis: policy.AxisCapture, Machine: scope.Machine, Category: category, Target: target.Key, Ancestors: target.Ancestors,
		MachineRules: machineRules, ProfileRules: profileRules, DefaultEnabled: true,
	})
	if err != nil {
		return policy.Effective{}, err
	}
	restore, err := policy.Resolve(policy.ResolveRequest{
		Axis: policy.AxisRestore, Machine: scope.Machine, Category: category, Target: target.Key, Ancestors: target.Ancestors,
		MachineRules: machineRules, ProfileRules: profileRules, DefaultEnabled: true,
	})
	if err != nil {
		return policy.Effective{}, err
	}
	return policy.Effective{Capture: capture, Restore: restore}, nil
}

// remapLegacyPreinstallPolicyRules keeps schema-12 generic package policy
// authority intact when an installed Omarchy catalogue later gives that
// package the canonical preinstall:<id> identity. It is deliberately scoped
// to an inspected preinstall target: generic package rules retain their
// normal meaning everywhere else. A canonical rule wins over its legacy
// alias; conflicting official/aur aliases fail closed rather than guessing.
func remapLegacyPreinstallPolicyRules(rules policy.Rules, category, target string) (policy.Rules, error) {
	if category != "packages" || !strings.HasPrefix(target, "preinstall:") {
		return rules, nil
	}
	id := strings.TrimPrefix(target, "preinstall:")
	aliases := map[string]bool{"official:" + id: true, "aur:" + id: true}
	remap := func(items []policy.Rule) ([]policy.Rule, error) {
		var direct *policy.Rule
		var alias *policy.Rule
		kept := make([]policy.Rule, 0, len(items))
		for _, rule := range items {
			if rule.Category != category || (!aliases[rule.Target] && rule.Target != target) {
				kept = append(kept, rule)
				continue
			}
			if rule.Target == target {
				copy := rule
				direct = &copy
				continue
			}
			if alias != nil && alias.Setting != rule.Setting {
				return nil, fmt.Errorf("workflow: conflicting legacy package policy aliases for %s", target)
			}
			copy := rule
			alias = &copy
		}
		if direct != nil {
			kept = append(kept, *direct)
		} else if alias != nil {
			alias.Target = target
			kept = append(kept, *alias)
		}
		return kept, nil
	}
	var err error
	rules.Capture, err = remap(rules.Capture)
	if err != nil {
		return policy.Rules{}, err
	}
	rules.Restore, err = remap(rules.Restore)
	if err != nil {
		return policy.Rules{}, err
	}
	return rules, nil
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

// resolveRestoreTarget resolves one target's effective Restore policy as
// viewed from the session's currently selected machine (Restore always
// plans for the machine running Blueprint, unlike EffectivePolicy's general
// TUI-facing scope), and derives the final RestoreDecision: a target the
// provider itself reports ineligible for Restore (RestoreEligible false)
// resolves to Skip regardless of what policy says, since provider safety
// checks remain authoritative -- carrying the provider's own SafetyReason
// rather than a policy-derived one. Otherwise the resolved policy decides,
// carrying RestoreSkipReason's standardized explanation when it resolves to
// Skip. It returns the raw resolved policy.EffectiveSetting alongside the
// decision so callers can show why, without a second resolution.
func (s *Session) resolveRestoreTarget(ctx context.Context, category string, target TargetInspection) (policy.EffectiveSetting, RestoreDecision, error) {
	effective, err := s.EffectivePolicy(ctx, PolicyScope{Machine: s.machine.Name}, category, target)
	if err != nil {
		return policy.EffectiveSetting{}, RestoreDecision{}, err
	}
	if !target.RestoreEligible {
		return effective.Restore, RestoreDecision{Restore: false, Resolved: true, Reason: target.SafetyReason}, nil
	}
	if !effective.Restore.Enabled {
		return effective.Restore, RestoreDecision{Restore: false, Resolved: true, Reason: RestoreSkipReason(effective.Restore)}, nil
	}
	return effective.Restore, RestoreDecision{Restore: true, Resolved: true}, nil
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

// validateLoadedPolicyTargets validates every target-shaped rule already
// present in data (the portable profile's Policy plus every machine's
// Policy) against its category's provider, once providers is the real
// registry SetProviders was just handed. A category with no registered
// provider is skipped rather than treated as an error: providers is not
// guaranteed to cover every category profile.Load's structural validation
// accepts (e.g. a caller inspecting a narrowed provider subset).
func validateLoadedPolicyTargets(providers []Provider, data profile.Data) error {
	if err := validatePolicyRuleTargets(providers, data.Policy, ""); err != nil {
		return err
	}
	for _, m := range data.Machines.Items {
		if err := validatePolicyRuleTargets(providers, m.Policy, m.Name); err != nil {
			return err
		}
	}
	return nil
}

func validatePolicyRuleTargets(providers []Provider, rules policy.Rules, machine string) error {
	if err := validateAxisRuleTargets(providers, rules.Capture, machine); err != nil {
		return err
	}
	return validateAxisRuleTargets(providers, rules.Restore, machine)
}

func validateAxisRuleTargets(providers []Provider, rules []policy.Rule, machine string) error {
	for _, rule := range rules {
		if rule.Target == "" {
			continue
		}
		provider, ok := ProviderByID(providers, rule.Category)
		if !ok {
			continue
		}
		canonical, err := validateTarget(provider, rule.Target)
		if err != nil {
			return policyTargetLoadError(machine, rule.Category, rule.Target, err)
		}
		// A syntactically valid but non-canonical target (e.g. Config's
		// "~/.config/nvim" instead of the stored ".config/nvim" key) must
		// fail loudly rather than load as-is: it would never match the
		// canonical target real inspection/resolution emits for the same
		// path, silently orphaning the rule. Per the approved human-edit
		// semantics, a hand-edited profile must contain exactly the
		// canonical form, not merely an equivalent one.
		if canonical != rule.Target {
			return policyTargetLoadError(machine, rule.Category, rule.Target,
				fmt.Errorf("not canonical: provider canonicalizes it to %q", canonical))
		}
	}
	return nil
}

func policyTargetLoadError(machine, category, target string, err error) error {
	if machine == "" {
		return fmt.Errorf("workflow: profile policy: category %q target %q: %w", category, target, err)
	}
	return fmt.Errorf("workflow: machine %q policy: category %q target %q: %w", machine, category, target, err)
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

// upsertPolicyRule and removePolicyRule always build a fresh backing array
// for the axis they touch, rather than mutating or truncate-reusing the
// caller's existing slice in place: rules is a shallow copy of a stored
// policy.Rules (the session's own, or a candidate "next" state a caller is
// building before a save that might still fail), and its Capture/Restore
// slices share a backing array with whatever it was copied from. Reusing
// that array here would corrupt the original through the shared backing
// array before the caller's save has even been attempted.
func upsertPolicyRule(rules policy.Rules, axis policy.Axis, rule policy.Rule) policy.Rules {
	list := axisRuleList(&rules, axis)
	next := make([]policy.Rule, len(*list), len(*list)+1)
	copy(next, *list)
	for i := range next {
		if next[i].Category == rule.Category && next[i].Target == rule.Target {
			next[i].Setting = rule.Setting
			*list = next
			return rules
		}
	}
	*list = append(next, rule)
	return rules
}

func removePolicyRule(rules policy.Rules, axis policy.Axis, category, target string) policy.Rules {
	list := axisRuleList(&rules, axis)
	next := make([]policy.Rule, 0, len(*list))
	for _, rule := range *list {
		if rule.Category == category && rule.Target == target {
			continue
		}
		next = append(next, rule)
	}
	*list = next
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
// Excluding strips ref's desired state (Official/AUR/Mise presence, and any
// existing Absent tombstone -- see profile.RemoveDesiredPackageState)
// immediately: the exclusion has no desired state at all, matching the
// design's "Capture: Preserve/disabled, Restore: Skip/disabled, Desired
// state: unmanaged" -- and records Capture Disabled + Restore Disabled
// policy for it, so a later enabled Capture does not silently re-adopt it
// (Merge's disabled row preserves whatever was desired; stripping first is
// what makes that "nothing"). Including clears those policy rules; it does
// not attempt to restore any prior desired state, since excluding left
// none to restore -- a later Capture will naturally rediscover the ref if
// it is still installed.
//
// The desired-state removal and both policy axes are built into one
// candidate profile.Data and saved once: a failure partway through building
// that candidate never reaches disk, and s.profile is only updated after
// the save succeeds, so a caller never observes "desired state removed but
// only one policy axis persisted."
func (s *Session) SetPackageExcluded(ref string, excluded bool) error {
	provider, ok := ProviderByID(s.providers, "packages")
	if !ok {
		return fmt.Errorf("workflow: packages provider is unavailable")
	}
	canonical, err := validateTarget(provider, ref)
	if err != nil {
		return err
	}
	if s.deps.Now == nil {
		return errors.New("workflow clock is unavailable")
	}
	next := s.profile
	if excluded {
		profile.RemoveDesiredPackageState(&next.Packages, canonical)
		next.Policy = upsertPolicyRule(next.Policy, policy.AxisCapture, policy.Rule{Category: "packages", Target: canonical, Setting: policy.SettingDisabled})
		next.Policy = upsertPolicyRule(next.Policy, policy.AxisRestore, policy.Rule{Category: "packages", Target: canonical, Setting: policy.SettingDisabled})
	} else {
		next.Policy = removePolicyRule(next.Policy, policy.AxisCapture, "packages", canonical)
		next.Policy = removePolicyRule(next.Policy, policy.AxisRestore, "packages", canonical)
	}
	next.Manifest.Profile.UpdatedAt = s.deps.Now().UTC()
	if err := profile.Save(s.opts.ProfileDir, next); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	s.profile = next
	return nil
}
