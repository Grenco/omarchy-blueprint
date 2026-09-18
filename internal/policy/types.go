// Package policy is the pure target-policy domain shared by workflow and
// providers: inheritance-aware Capture/Restore settings and restore-intent
// axes for provider-defined target keys. It has no profile, workflow, or
// provider imports.
package policy

import "fmt"

// Setting is an explicit or resolved Capture/Restore setting for a target.
type Setting string

const (
	SettingEnabled  Setting = "enabled"
	SettingDisabled Setting = "disabled"
)

// ValidateSetting reports whether setting is one of the accepted values.
func ValidateSetting(setting Setting) error {
	switch setting {
	case SettingEnabled, SettingDisabled:
		return nil
	default:
		return fmt.Errorf("policy: invalid setting %q, want %q or %q", setting, SettingEnabled, SettingDisabled)
	}
}

// Axis distinguishes Capture policy from Restore policy; the two are
// resolved independently for the same target.
type Axis string

const (
	AxisCapture Axis = "capture"
	AxisRestore Axis = "restore"
)

// ValidateAxis reports whether axis is one of the accepted values. Resolve
// rejects any other value rather than silently defaulting to one axis's
// rules, since a wrong/unknown axis reaching rule selection could consult
// the wrong policy (for example Capture rules for a Restore resolution).
func ValidateAxis(axis Axis) error {
	switch axis {
	case AxisCapture, AxisRestore:
		return nil
	default:
		return fmt.Errorf("policy: invalid axis %q, want %q or %q", axis, AxisCapture, AxisRestore)
	}
}

// Rule is one persisted policy override. Target is empty for a
// category-level rule and non-empty for a target-specific rule.
type Rule struct {
	Category string  `toml:"category" json:"category"`
	Target   string  `toml:"target,omitempty" json:"target,omitempty"`
	Setting  Setting `toml:"setting" json:"setting"`
}

// Rules is the sparse set of Capture/Restore overrides for one policy
// scope (portable profile or one machine).
type Rules struct {
	Capture []Rule `toml:"capture,omitempty" json:"capture,omitempty"`
	Restore []Rule `toml:"restore,omitempty" json:"restore,omitempty"`
}

// SourceKind identifies which precedence level produced an EffectiveSetting.
type SourceKind string

// Source identifies where a resolved setting came from, for presentation
// and debugging; it never affects the resolved value itself.
type Source struct {
	Kind     SourceKind `json:"kind"`
	Machine  string     `json:"machine,omitempty"`
	Category string     `json:"category"`
	Target   string     `json:"target,omitempty"`
}

// EffectiveSetting is the resolved outcome for one axis of one target.
// Explicit is true when the value came from a direct target/category rule
// at the selected scope, and false when it was inherited from an ancestor
// or the provider default.
type EffectiveSetting struct {
	Enabled  bool   `json:"enabled"`
	Source   Source `json:"source"`
	Explicit bool   `json:"explicit"`
}

// Effective bundles both resolved axes for one target.
type Effective struct {
	Capture EffectiveSetting `json:"capture"`
	Restore EffectiveSetting `json:"restore"`
}

// ConflictMode is the Restore conflict-handling axis, independent of
// ConvergenceMode.
type ConflictMode string

const (
	ConflictSafe  ConflictMode = "safe"
	ConflictForce ConflictMode = "force"
)

// ConvergenceMode is the Restore convergence axis, independent of
// ConflictMode.
type ConvergenceMode string

const (
	ConvergenceAdditive ConvergenceMode = "additive"
	ConvergenceExact    ConvergenceMode = "exact"
)

// RestoreOptions carries one restore run's two independent intent axes.
type RestoreOptions struct {
	Conflicts   ConflictMode
	Convergence ConvergenceMode
}

// DefaultRestoreOptions is the built-in Safe+Additive restore intent used
// when no machine default or one-run override applies.
func DefaultRestoreOptions() RestoreOptions {
	return RestoreOptions{Conflicts: ConflictSafe, Convergence: ConvergenceAdditive}
}

// ValidateRestoreOptions reports whether both axes hold an accepted value.
// It does not normalize empty/zero-value fields; empty persisted machine
// restore fields are normalized to defaults by profile/workflow before
// reaching this validation.
func ValidateRestoreOptions(options RestoreOptions) error {
	switch options.Conflicts {
	case ConflictSafe, ConflictForce:
	default:
		return fmt.Errorf("policy: invalid conflict mode %q, want %q or %q", options.Conflicts, ConflictSafe, ConflictForce)
	}
	switch options.Convergence {
	case ConvergenceAdditive, ConvergenceExact:
	default:
		return fmt.Errorf("policy: invalid convergence mode %q, want %q or %q", options.Convergence, ConvergenceAdditive, ConvergenceExact)
	}
	return nil
}
