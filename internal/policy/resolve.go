package policy

import "fmt"

const (
	SourceMachineTarget   SourceKind = "machine-target"
	SourceMachineAncestor SourceKind = "machine-ancestor"
	SourceMachineCategory SourceKind = "machine-category"
	SourceProfileTarget   SourceKind = "profile-target"
	SourceProfileAncestor SourceKind = "profile-ancestor"
	SourceProfileCategory SourceKind = "profile-category"
	SourceDefault         SourceKind = "default"
)

// ResolveRequest describes one target's Capture or Restore policy lookup.
// Ancestors lists the target's parent chain, nearest parent first, for
// hierarchical providers such as Config; non-hierarchical targets pass nil.
type ResolveRequest struct {
	Axis           Axis
	Machine        string
	Category       string
	Target         string
	Ancestors      []string
	MachineRules   Rules
	ProfileRules   Rules
	DefaultEnabled bool
}

// Resolve applies the shared inheritance precedence for one axis of one
// target: machine target, nearest machine ancestor, machine category,
// profile target, nearest profile ancestor, profile category, then the
// provider default. Providers must not implement this precedence
// themselves.
func Resolve(request ResolveRequest) (EffectiveSetting, error) {
	machineRules := axisRules(request.MachineRules, request.Axis)
	profileRules := axisRules(request.ProfileRules, request.Axis)

	if err := validateRules(machineRules); err != nil {
		return EffectiveSetting{}, fmt.Errorf("policy: machine rules: %w", err)
	}
	if err := validateRules(profileRules); err != nil {
		return EffectiveSetting{}, fmt.Errorf("policy: profile rules: %w", err)
	}

	if setting, ok := findTarget(machineRules, request.Category, request.Target); ok {
		return EffectiveSetting{
			Enabled:  setting == SettingEnabled,
			Explicit: true,
			Source:   Source{Kind: SourceMachineTarget, Machine: request.Machine, Category: request.Category, Target: request.Target},
		}, nil
	}
	if setting, ancestor, ok := findAncestor(machineRules, request.Category, request.Ancestors); ok {
		return EffectiveSetting{
			Enabled:  setting == SettingEnabled,
			Explicit: false,
			Source:   Source{Kind: SourceMachineAncestor, Machine: request.Machine, Category: request.Category, Target: ancestor},
		}, nil
	}
	if setting, ok := findCategory(machineRules, request.Category); ok {
		return EffectiveSetting{
			Enabled:  setting == SettingEnabled,
			Explicit: true,
			Source:   Source{Kind: SourceMachineCategory, Machine: request.Machine, Category: request.Category},
		}, nil
	}
	if setting, ok := findTarget(profileRules, request.Category, request.Target); ok {
		return EffectiveSetting{
			Enabled:  setting == SettingEnabled,
			Explicit: true,
			Source:   Source{Kind: SourceProfileTarget, Category: request.Category, Target: request.Target},
		}, nil
	}
	if setting, ancestor, ok := findAncestor(profileRules, request.Category, request.Ancestors); ok {
		return EffectiveSetting{
			Enabled:  setting == SettingEnabled,
			Explicit: false,
			Source:   Source{Kind: SourceProfileAncestor, Category: request.Category, Target: ancestor},
		}, nil
	}
	if setting, ok := findCategory(profileRules, request.Category); ok {
		return EffectiveSetting{
			Enabled:  setting == SettingEnabled,
			Explicit: true,
			Source:   Source{Kind: SourceProfileCategory, Category: request.Category},
		}, nil
	}
	return EffectiveSetting{
		Enabled:  request.DefaultEnabled,
		Explicit: false,
		Source:   Source{Kind: SourceDefault, Category: request.Category, Target: request.Target},
	}, nil
}

func axisRules(rules Rules, axis Axis) []Rule {
	if axis == AxisRestore {
		return rules.Restore
	}
	return rules.Capture
}

func validateRules(rules []Rule) error {
	seen := make(map[[2]string]bool, len(rules))
	for _, r := range rules {
		if err := ValidateSetting(r.Setting); err != nil {
			return err
		}
		key := [2]string{r.Category, r.Target}
		if seen[key] {
			return fmt.Errorf("duplicate rule for category %q target %q", r.Category, r.Target)
		}
		seen[key] = true
	}
	return nil
}

func findTarget(rules []Rule, category, target string) (Setting, bool) {
	if target == "" {
		return "", false
	}
	for _, r := range rules {
		if r.Category == category && r.Target == target {
			return r.Setting, true
		}
	}
	return "", false
}

func findAncestor(rules []Rule, category string, ancestors []string) (Setting, string, bool) {
	for _, ancestor := range ancestors {
		if ancestor == "" {
			continue
		}
		if setting, ok := findTarget(rules, category, ancestor); ok {
			return setting, ancestor, true
		}
	}
	return "", "", false
}

func findCategory(rules []Rule, category string) (Setting, bool) {
	for _, r := range rules {
		if r.Category == category && r.Target == "" {
			return r.Setting, true
		}
	}
	return "", false
}
