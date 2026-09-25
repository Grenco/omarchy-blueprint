package app

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func policyCommand(deps Dependencies, opt *options) *cobra.Command {
	cmd := &cobra.Command{Use: "policy", Short: "Inspect and edit Capture and Restore policy"}
	cmd.AddCommand(policyShowCommand(deps, opt), policySetCommand(deps, opt), policyClearCommand(deps, opt), policyStopCommand(deps, opt))
	return cmd
}

func policyScope(session *workflow.Session, name string, explicit bool) (workflow.PolicyScope, error) {
	if explicit && name == "" {
		return workflow.PolicyScope{}, fmt.Errorf("--scope must be profile or machine:<name>")
	}
	if !explicit {
		name = session.Machine().Name
	} else if name == "profile" {
		name = ""
	} else if strings.HasPrefix(name, "machine:") {
		name = strings.TrimPrefix(name, "machine:")
		if name == "" {
			return workflow.PolicyScope{}, fmt.Errorf("--scope machine:<name> requires a machine name")
		}
	}
	if name != "" {
		for _, item := range session.Profile().Machines.Items {
			if item.Name == name {
				return workflow.PolicyScope{Machine: name}, nil
			}
		}
		return workflow.PolicyScope{}, fmt.Errorf("machine %q does not exist", name)
	}
	return workflow.PolicyScope{}, nil
}

func policyScopeLabel(scope workflow.PolicyScope) string {
	if scope.Machine == "" {
		return "profile"
	}
	return "machine:" + scope.Machine
}

// CLI values describe user intent; workflow and on-disk policy retain their
// shared enabled/disabled representation. The axes are deliberately distinct.
func parsePolicyValue(axis policy.Axis, value string) (policy.Setting, error) {
	switch axis {
	case policy.AxisCapture:
		switch value {
		case "update":
			return policy.SettingEnabled, nil
		case "preserve":
			return policy.SettingDisabled, nil
		}
		return "", fmt.Errorf("invalid Capture policy %q; want update or preserve", value)
	case policy.AxisRestore:
		switch value {
		case "apply":
			return policy.SettingEnabled, nil
		case "skip":
			return policy.SettingDisabled, nil
		}
		return "", fmt.Errorf("invalid Restore policy %q; want apply or skip", value)
	default:
		return "", policy.ValidateAxis(axis)
	}
}

func policyValue(axis policy.Axis, enabled bool) string {
	if axis == policy.AxisCapture {
		if enabled {
			return "update"
		}
		return "preserve"
	}
	if enabled {
		return "apply"
	}
	return "skip"
}

type cliEffectiveSetting struct {
	Value    string        `json:"value"`
	Source   policy.Source `json:"source"`
	Explicit bool          `json:"explicit"`
}

func effectivePolicyValue(axis policy.Axis, setting policy.EffectiveSetting) cliEffectiveSetting {
	return cliEffectiveSetting{Value: policyValue(axis, setting.Enabled), Source: setting.Source, Explicit: setting.Explicit}
}

type cliPolicyEffective struct {
	Capture cliEffectiveSetting `json:"capture"`
	Restore cliEffectiveSetting `json:"restore"`
}

type cliPolicyRule struct {
	Category string `json:"category"`
	Target   string `json:"target,omitempty"`
	Value    string `json:"value"`
}

type cliPolicyRuleSet struct {
	Capture []cliPolicyRule `json:"capture"`
	Restore []cliPolicyRule `json:"restore"`
}

func cliPolicyRules(rules policy.Rules) cliPolicyRuleSet {
	converted := cliPolicyRuleSet{Capture: make([]cliPolicyRule, 0, len(rules.Capture)), Restore: make([]cliPolicyRule, 0, len(rules.Restore))}
	for _, rule := range rules.Capture {
		converted.Capture = append(converted.Capture, cliPolicyRule{Category: rule.Category, Target: rule.Target, Value: policyValue(policy.AxisCapture, rule.Setting == policy.SettingEnabled)})
	}
	for _, rule := range rules.Restore {
		converted.Restore = append(converted.Restore, cliPolicyRule{Category: rule.Category, Target: rule.Target, Value: policyValue(policy.AxisRestore, rule.Setting == policy.SettingEnabled)})
	}
	return converted
}

func policyProvider(deps Dependencies, opt *options, category string) (workflow.Provider, error) {
	provider, ok := workflow.ProviderByID(workflowProviders(deps, opt), category)
	if !ok {
		return nil, fmt.Errorf("unknown policy category %q", category)
	}
	return provider, nil
}

func canonicalPolicyTarget(provider workflow.Provider, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if validator, ok := provider.(interface{ ValidateTarget(string) (string, error) }); ok {
		return validator.ValidateTarget(raw)
	}
	return raw, nil
}

func policyShowCommand(deps Dependencies, opt *options) *cobra.Command {
	var scopeName string
	cmd := &cobra.Command{Use: "show [category [target]]", Args: cobra.MaximumNArgs(2), Short: "Show explicit rules and effective policy", RunE: func(cmd *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		scope, err := policyScope(session, scopeName, cmd.Flags().Changed("scope"))
		if err != nil {
			return err
		}
		rules := session.Profile().Policy
		if scope.Machine != "" {
			for _, item := range session.Profile().Machines.Items {
				if item.Name == scope.Machine {
					rules = item.Policy
					break
				}
			}
		}
		category, target := "", ""
		if len(args) > 0 {
			category = args[0]
		}
		if len(args) > 1 {
			target = args[1]
		}
		if category != "" {
			provider, err := policyProvider(deps, opt, category)
			if err != nil {
				return err
			}
			target, err = canonicalPolicyTarget(provider, target)
			if err != nil {
				return err
			}
		}
		type entry struct {
			Category   string                     `json:"category"`
			Target     string                     `json:"target,omitempty"`
			Inspection *workflow.TargetInspection `json:"inspection,omitempty"`
			Effective  cliPolicyEffective         `json:"effective"`
		}
		items := make([]entry, 0)
		providers := workflowProviders(deps, opt)
		for _, provider := range providers {
			if category != "" && category != provider.ID() {
				continue
			}
			inventory, err := session.PolicyTargets(cmd.Context(), provider.ID())
			if err != nil {
				return fmt.Errorf("inspect %s targets: %w", provider.ID(), err)
			}
			byKey := map[string]workflow.TargetInspection{}
			for _, inspected := range inventory {
				byKey[inspected.Key] = inspected
			}
			keys := map[string]bool{"": true}
			if target != "" {
				keys = map[string]bool{target: true}
			} else {
				for key := range byKey {
					keys[key] = true
				}
				for _, rule := range append(append([]policy.Rule{}, rules.Capture...), rules.Restore...) {
					if rule.Category == provider.ID() {
						keys[rule.Target] = true
					}
				}
				for _, rule := range append(append([]policy.Rule{}, session.Profile().Policy.Capture...), session.Profile().Policy.Restore...) {
					if rule.Category == provider.ID() {
						keys[rule.Target] = true
					}
				}
			}
			ordered := make([]string, 0, len(keys))
			for key := range keys {
				ordered = append(ordered, key)
			}
			sort.Strings(ordered)
			for _, key := range ordered {
				inspected, found := byKey[key]
				if !found {
					inspected = workflow.TargetInspection{Key: key}
					if provider.ID() == "config" && key != "" {
						for parent := path.Dir(key); parent != "." && parent != "/"; parent = path.Dir(parent) {
							inspected.Ancestors = append(inspected.Ancestors, parent)
						}
					}
				}
				effective, err := session.EffectivePolicy(cmd.Context(), scope, provider.ID(), inspected)
				if err != nil {
					return err
				}
				item := entry{Category: provider.ID(), Target: key, Effective: cliPolicyEffective{
					Capture: effectivePolicyValue(policy.AxisCapture, effective.Capture),
					Restore: effectivePolicyValue(policy.AxisRestore, effective.Restore),
				}}
				if found {
					item.Inspection = &inspected
				}
				items = append(items, item)
			}
		}
		var human strings.Builder
		fmt.Fprintf(&human, "Policy scope: %s\n", policyScopeLabel(scope))
		for _, item := range items {
			label := item.Category
			if item.Target != "" {
				label += "/" + item.Target
			}
			fmt.Fprintf(&human, "%s  Capture: %s (%s)  Restore: %s (%s)\n", label, strings.ToUpper(item.Effective.Capture.Value[:1])+item.Effective.Capture.Value[1:], item.Effective.Capture.Source.Kind, strings.ToUpper(item.Effective.Restore.Value[:1])+item.Effective.Restore.Value[1:], item.Effective.Restore.Source.Kind)
		}
		return emit(deps.Out, opt.json, "policy show", true, map[string]any{"scope": policyScopeLabel(scope), "rules": cliPolicyRules(rules), "targets": items}, human.String())
	}}
	cmd.Flags().StringVar(&scopeName, "scope", "", "policy scope: profile or machine:<name> (default: active machine)")
	return cmd
}

func policySetCommand(deps Dependencies, opt *options) *cobra.Command {
	var scopeName string
	cmd := &cobra.Command{Use: "set <capture|restore> <category> <update|preserve|apply|skip> [target]", Args: cobra.RangeArgs(3, 4), Short: "Set an explicit category or target override", RunE: func(cmd *cobra.Command, args []string) error {
		axis := policy.Axis(args[0])
		if err := policy.ValidateAxis(axis); err != nil {
			return err
		}
		setting, err := parsePolicyValue(axis, args[2])
		if err != nil {
			return err
		}
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		scope, err := policyScope(session, scopeName, cmd.Flags().Changed("scope"))
		if err != nil {
			return err
		}
		target := ""
		if len(args) == 4 {
			target = args[3]
		}
		provider, err := policyProvider(deps, opt, args[1])
		if err != nil {
			return err
		}
		target, err = canonicalPolicyTarget(provider, target)
		if err != nil {
			return err
		}
		if err := session.SetPolicy(scope, axis, args[1], target, setting); err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "policy set", true, map[string]any{"scope": policyScopeLabel(scope), "axis": axis, "category": args[1], "target": target, "value": args[2]}, fmt.Sprintf("Set %s %s/%s to %s at %s scope.\n", axis, args[1], target, args[2], policyScopeLabel(scope)))
	}}
	cmd.Flags().StringVar(&scopeName, "scope", "", "policy scope: profile or machine:<name> (default: active machine)")
	return cmd
}

func policyClearCommand(deps Dependencies, opt *options) *cobra.Command {
	var scopeName string
	cmd := &cobra.Command{Use: "clear <capture|restore> <category> [target]", Args: cobra.RangeArgs(2, 3), Short: "Reset an override to inherited policy", RunE: func(cmd *cobra.Command, args []string) error {
		axis := policy.Axis(args[0])
		if err := policy.ValidateAxis(axis); err != nil {
			return err
		}
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		scope, err := policyScope(session, scopeName, cmd.Flags().Changed("scope"))
		if err != nil {
			return err
		}
		provider, err := policyProvider(deps, opt, args[1])
		if err != nil {
			return err
		}
		target := ""
		if len(args) == 3 {
			target = args[2]
		}
		target, err = canonicalPolicyTarget(provider, target)
		if err != nil {
			return err
		}
		if err := session.ClearPolicy(scope, axis, args[1], target); err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "policy clear", true, map[string]any{"scope": policyScopeLabel(scope), "axis": axis, "category": args[1], "target": target}, fmt.Sprintf("Cleared %s %s/%s at %s scope.\n", axis, args[1], target, policyScopeLabel(scope)))
	}}
	cmd.Flags().StringVar(&scopeName, "scope", "", "policy scope: profile or machine:<name> (default: active machine)")
	return cmd
}

func policyStopCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "stop-managing <category> <target>", Args: cobra.ExactArgs(2), Short: "Forget a provider-owned target and its overrides", RunE: func(cmd *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		provider, err := policyProvider(deps, opt, args[0])
		if err != nil {
			return err
		}
		target, err := canonicalPolicyTarget(provider, args[1])
		if err != nil {
			return err
		}
		if err := session.StopManaging(cmd.Context(), args[0], target); err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "policy stop-managing", true, map[string]any{"category": args[0], "target": target}, fmt.Sprintf("Stopped managing %s/%s.\n", args[0], target))
	}}
}

func machineRestoreDefaultsCommand(deps Dependencies, opt *options) *cobra.Command {
	cmd := &cobra.Command{Use: "restore-defaults [name]", Args: cobra.MaximumNArgs(1), Short: "Show persisted machine Restore defaults", RunE: func(_ *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		name := session.Machine().Name
		if len(args) > 0 {
			name = args[0]
		}
		m, err := namedMachine(session.Profile(), name)
		if err != nil {
			return err
		}
		options := m.EffectiveRestoreDefaults()
		return emit(deps.Out, opt.json, "machine restore-defaults", true, map[string]any{"machine": name, "conflicts": options.Conflicts, "convergence": options.Convergence}, fmt.Sprintf("%s Restore defaults: %s / %s\n", name, options.Conflicts, options.Convergence))
	}}
	var conflicts, convergence string
	set := &cobra.Command{Use: "set <name>", Args: cobra.ExactArgs(1), Short: "Set persisted Restore defaults for a machine", RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("conflicts") && !cmd.Flags().Changed("convergence") {
			return fmt.Errorf("specify --conflicts and/or --convergence")
		}
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		m, err := namedMachine(session.Profile(), args[0])
		if err != nil {
			return err
		}
		options := m.EffectiveRestoreDefaults()
		if cmd.Flags().Changed("conflicts") {
			options.Conflicts = policy.ConflictMode(conflicts)
		}
		if cmd.Flags().Changed("convergence") {
			options.Convergence = policy.ConvergenceMode(convergence)
		}
		if err := session.SetMachineRestoreDefaults(args[0], options); err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "machine restore-defaults set", true, map[string]any{"machine": args[0], "conflicts": options.Conflicts, "convergence": options.Convergence}, fmt.Sprintf("%s Restore defaults: %s / %s\n", args[0], options.Conflicts, options.Convergence))
	}}
	set.Flags().StringVar(&conflicts, "conflicts", "", "safe or force")
	set.Flags().StringVar(&convergence, "convergence", "", "additive or exact")
	cmd.AddCommand(set)
	return cmd
}

func namedMachine(data profile.Data, name string) (profile.Machine, error) {
	for _, item := range data.Machines.Items {
		if name != "" && item.Name == name {
			return item, nil
		}
	}
	if name == "" {
		return profile.Machine{}, fmt.Errorf("no machine selected; specify a machine name")
	}
	return profile.Machine{}, fmt.Errorf("machine %q does not exist", name)
}
