package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

// policyCategories is the known category set hand-edited policy records may
// reference. It mirrors the provider categories the workflow/app layers
// register; this package does not import those layers, so the set is
// declared directly here.
var policyCategories = map[string]bool{
	"packages":  true,
	"themes":    true,
	"plugins":   true,
	"config":    true,
	"defaults":  true,
	"shell":     true,
	"hooks":     true,
	"resources": true,
}

// loadPolicy reads the portable policy overrides. A missing file means no
// overrides and is valid.
func loadPolicy(path string) (policy.Rules, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return policy.Rules{}, nil
	}
	if err != nil {
		return policy.Rules{}, err
	}
	var rules policy.Rules
	if err := toml.Unmarshal(b, &rules); err != nil {
		return policy.Rules{}, fmt.Errorf("parse policy/policy.toml: %w", err)
	}
	if err := validatePolicyRules(rules); err != nil {
		return policy.Rules{}, fmt.Errorf("policy/policy.toml: %w", err)
	}
	return rules, nil
}

// normalizePolicyRules canonicalizes rules in place (deterministic sort, so
// Git diffs are stable) and validates them. The shared policy package owns
// semantic Setting validation; this is the profile-layer structural
// validation Task 12 assigns to profile: known category, and no duplicate or
// conflicting records for the same (axis, category, target).
func normalizePolicyRules(rules *policy.Rules) error {
	sortRules(rules.Capture)
	sortRules(rules.Restore)
	return validatePolicyRules(*rules)
}

func validatePolicyRules(rules policy.Rules) error {
	if err := validateRuleAxis(rules.Capture); err != nil {
		return fmt.Errorf("capture: %w", err)
	}
	if err := validateRuleAxis(rules.Restore); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	return nil
}

func validateRuleAxis(rules []policy.Rule) error {
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		if !policyCategories[rule.Category] {
			return fmt.Errorf("unknown policy category %q", rule.Category)
		}
		if err := policy.ValidateSetting(rule.Setting); err != nil {
			return err
		}
		key := rule.Category + "\x00" + rule.Target
		if seen[key] {
			if rule.Target == "" {
				return fmt.Errorf("duplicate policy rule for category %q", rule.Category)
			}
			return fmt.Errorf("duplicate policy rule for category %q target %q", rule.Category, rule.Target)
		}
		seen[key] = true
	}
	return nil
}

func sortRules(rules []policy.Rule) {
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Category != rules[j].Category {
			return rules[i].Category < rules[j].Category
		}
		return rules[i].Target < rules[j].Target
	})
}

// validateMachineRestoreDefaults validates a machine's sparse restore-mode
// overrides. An empty value means "no override" and is always valid; a
// non-empty value must be one of the accepted settings for its axis.
func validateMachineRestoreDefaults(conflicts policy.ConflictMode, convergence policy.ConvergenceMode) error {
	switch conflicts {
	case "", policy.ConflictSafe, policy.ConflictForce:
	default:
		return fmt.Errorf("invalid restore_conflicts %q, want %q or %q", conflicts, policy.ConflictSafe, policy.ConflictForce)
	}
	switch convergence {
	case "", policy.ConvergenceAdditive, policy.ConvergenceExact:
	default:
		return fmt.Errorf("invalid restore_convergence %q, want %q or %q", convergence, policy.ConvergenceAdditive, policy.ConvergenceExact)
	}
	return nil
}

// EffectiveRestoreDefaults normalizes this machine's sparse restore-mode
// overrides to the built-in Safe/Additive default for whichever axis has no
// override, per the design's "missing restore-mode fields resolve to
// Safe + Additive."
func (m Machine) EffectiveRestoreDefaults() policy.RestoreOptions {
	options := policy.DefaultRestoreOptions()
	if m.RestoreConflicts != "" {
		options.Conflicts = m.RestoreConflicts
	}
	if m.RestoreConvergence != "" {
		options.Convergence = m.RestoreConvergence
	}
	return options
}

// savePolicyFile writes policy/policy.toml, or removes it when rules is
// empty: only overrides need to be written, and a lingering empty file would
// be pure Git noise for a profile with no policy overrides at all.
// migrateLegacyPackageExclusions converts a legacy (pre-schema-12) Packages
// profile into the current policy-based model, in memory only:
//
//   - each Packages.Excluded ref becomes a portable Capture Disabled +
//     Restore Disabled policy rule pair, so the target is unmanaged with no
//     desired state -- not a desired-absence tombstone;
//   - Packages.MachineSpecific is cleared, since it becomes pure runtime
//     inspection/safety metadata (rediscovered locally by the packages
//     provider) rather than portable desired state.
//
// This never consults the current machine to infer desired absence: a
// legacy desired-present item stays desired-present.
func migrateLegacyPackageExclusions(d *Data) {
	for _, ref := range d.Packages.Excluded {
		d.Policy = upsertRuleIfMissing(d.Policy, policy.AxisCapture, policy.Rule{Category: "packages", Target: ref, Setting: policy.SettingDisabled})
		d.Policy = upsertRuleIfMissing(d.Policy, policy.AxisRestore, policy.Rule{Category: "packages", Target: ref, Setting: policy.SettingDisabled})
	}
	d.Packages.Excluded = nil
	d.Packages.MachineSpecific = nil
}

func upsertRuleIfMissing(rules policy.Rules, axis policy.Axis, rule policy.Rule) policy.Rules {
	list := rules.Capture
	if axis == policy.AxisRestore {
		list = rules.Restore
	}
	for _, existing := range list {
		if existing.Category == rule.Category && existing.Target == rule.Target {
			return rules
		}
	}
	if axis == policy.AxisRestore {
		rules.Restore = append(rules.Restore, rule)
	} else {
		rules.Capture = append(rules.Capture, rule)
	}
	return rules
}

func savePolicyFile(dir string, rules policy.Rules) error {
	path := filepath.Join(dir, "policy", "policy.toml")
	if len(rules.Capture) == 0 && len(rules.Restore) == 0 {
		err := os.Remove(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := toml.Marshal(rules)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "policy"), 0o755); err != nil {
		return err
	}
	return atomicWrite(path, b)
}
