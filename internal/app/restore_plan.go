package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	pluginsprovider "github.com/Grenco/omarchy-blueprint/internal/providers/plugins"
	shellprovider "github.com/Grenco/omarchy-blueprint/internal/providers/shell"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

// restorePlanOptionsFromPolicy derives the finalizer's Force flag from the
// Conflicts axis only. Convergence (Additive/Exact) must stay irrelevant to
// Shell/plugin dependency conflict resolution: whether a required plugin is
// missing or provenance-mismatched is a Safe/Force question, never an
// Additive/Exact one.
func restorePlanOptionsFromPolicy(options policy.RestoreOptions) restorePlanOptions {
	return restorePlanOptions{Force: options.Conflicts == policy.ConflictForce}
}

func providerSelected(providers []stateProvider, id string) bool {
	for _, provider := range providers {
		if provider.ID() == id {
			return true
		}
	}
	return false
}

func findOperationIndex(plan *model.RestorePlan, provider, action string) int {
	for i, operation := range plan.Operations {
		if operation.Provider == provider && operation.Action == action {
			return i
		}
	}
	return -1
}

func lastPluginOperationID(plan model.RestorePlan, pluginID string) string {
	resource := "plugin:" + pluginID
	last := ""
	for _, operation := range plan.Operations {
		if operation.Provider == "plugins" && operation.Resource == resource {
			last = operation.ID
		}
	}
	return last
}

// blockPluginRemovalsReferencedByEffectiveShell prevents Exact from
// deleting a third-party plugin the machine's actual, currently effective
// Shell configuration still references -- independent of whether this run
// is also restoring Shell, and independent of RequiredThirdPartyPlugins
// below (which only reports references a proposed Shell merge would newly
// introduce, never ones the live document already has). Plugin
// availability is still a prerequisite for an existing Shell reference
// even though Shell owns enablement/layout separately, so an existing
// reference is a removal-safety conflict: the removal becomes a visible
// skip instead of proceeding. Shell detection failing for any reason (no
// baseline configured, ShellPaths unset in a test double, ...) fails open
// -- there is no live Shell state on record to conflict with.
func blockPluginRemovalsReferencedByEffectiveShell(deps Dependencies, opt *options, plan *model.RestorePlan) {
	if deps.ShellPaths == nil {
		return
	}
	hasRemoval := false
	for _, operation := range plan.Operations {
		if operation.Provider == "plugins" && operation.Action == "remove" {
			hasRemoval = true
			break
		}
	}
	if !hasRemoval {
		return
	}
	baseline, user, err := deps.ShellPaths()
	if err != nil {
		return
	}
	current, err := (shellprovider.Provider{BaselinePath: baseline, UserPath: user, ProfileDir: opt.profileDir}).Detect()
	if err != nil {
		return
	}
	target := current.Baseline
	if current.UserExists {
		target = current.Current
	}
	referenced := make(map[string]bool, len(target.References))
	for _, id := range target.References {
		referenced[id] = true
	}
	kept := plan.Operations[:0]
	for _, operation := range plan.Operations {
		id := ""
		if len(operation.Items) > 0 {
			id = operation.Items[0]
		}
		if operation.Provider == "plugins" && operation.Action == "remove" && referenced[id] {
			plan.Skipped = append(plan.Skipped, model.Skipped{
				Provider: "plugins",
				Resource: operation.Resource,
				Reason:   fmt.Sprintf("plugin %q is still referenced by the effective Shell configuration; removal disabled", id),
			})
			continue
		}
		kept = append(kept, operation)
	}
	plan.Operations = kept
}

// finalizeRestorePlan links Shell configuration writes to source reconstruction
// for any third-party plugins the captured document references.
func finalizeRestorePlan(ctx context.Context, deps Dependencies, opt *options, d profile.Data, providers []stateProvider, plan *model.RestorePlan, options restorePlanOptions) error {
	blockPluginRemovalsReferencedByEffectiveShell(deps, opt, plan)
	if !providerSelected(providers, "shell") {
		return restore.ValidatePlan(*plan)
	}
	writeIndex := findOperationIndex(plan, "shell", "write")
	if writeIndex < 0 {
		return restore.ValidatePlan(*plan)
	}
	shell, err := (shellStateProvider{deps: deps, opt: opt}).provider()
	if err != nil {
		return err
	}
	currentShell, err := shell.Detect()
	if err != nil {
		return err
	}
	required, err := shell.RequiredThirdPartyPlugins(d.Shell, currentShell, shellprovider.MergeOptions{Force: options.Force})
	if err != nil {
		return err
	}
	plugins, err := pluginProvider(deps, opt)
	if err != nil {
		return err
	}
	current, err := plugins.Detect(ctx)
	if err != nil {
		return err
	}
	installed := make(map[string]profile.Plugin, len(current.Items))
	for _, item := range current.Items {
		installed[item.ID] = item
	}
	saved := make(map[string]profile.Plugin, len(d.Plugins.Items))
	for _, item := range d.Plugins.Items {
		saved[item.ID] = item
	}
	var blocked, conflicts, dependencies []string
	for _, id := range required {
		wanted, captured := saved[id]
		if item, ok := installed[id]; ok && captured && pluginsprovider.Equivalent(wanted, item) {
			continue
		}
		if _, ok := installed[id]; ok {
			conflicts = append(conflicts, id)
			continue
		}
		if !providerSelected(providers, "plugins") {
			blocked = append(blocked, id)
			continue
		}
		dependency := lastPluginOperationID(*plan, id)
		if dependency == "" {
			blocked = append(blocked, id)
			continue
		}
		dependencies = appendUnique(dependencies, dependency)
	}
	if len(blocked) > 0 || len(conflicts) > 0 {
		sort.Strings(blocked)
		sort.Strings(conflicts)
		kept := plan.Operations[:0]
		for _, operation := range plan.Operations {
			if operation.Provider != "shell" {
				kept = append(kept, operation)
			}
		}
		plan.Operations = kept
		reason := ""
		if len(conflicts) > 0 {
			reason = fmt.Sprintf("required plugin(s) %s are installed but differ from captured provenance; Shell restore blocked to avoid activating unexpected code", strings.Join(conflicts, ", "))
		} else {
			reason = fmt.Sprintf("required plugin(s) %s are not installed and cannot be restored in this plan; restore plugins first or run aggregate restore", strings.Join(blocked, ", "))
		}
		plan.Skipped = append(plan.Skipped, model.Skipped{
			Provider: "shell",
			Resource: "shell:config",
			Reason:   reason,
		})
		return restore.ValidatePlan(*plan)
	}
	plan.Operations[writeIndex].DependsOn = appendUnique(plan.Operations[writeIndex].DependsOn, dependencies...)
	return restore.ValidatePlan(*plan)
}

func appendUnique(items []string, values ...string) []string {
	seen := make(map[string]bool, len(items)+len(values))
	for _, item := range items {
		seen[item] = true
	}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			items = append(items, value)
		}
	}
	return items
}
