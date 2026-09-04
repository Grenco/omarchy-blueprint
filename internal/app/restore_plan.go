package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

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

// finalizeRestorePlan links Shell configuration writes to source reconstruction
// for any third-party plugins the captured document references.
func finalizeRestorePlan(ctx context.Context, deps Dependencies, opt *options, d profile.Data, providers []stateProvider, plan *model.RestorePlan) error {
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
	required, err := shell.RequiredThirdPartyPlugins(d.Shell)
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
	installed := make(map[string]bool, len(current.Items))
	for _, item := range current.Items {
		installed[item.ID] = true
	}
	var blocked, dependencies []string
	for _, id := range required {
		if installed[id] {
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
	if len(blocked) > 0 {
		sort.Strings(blocked)
		kept := plan.Operations[:0]
		for _, operation := range plan.Operations {
			if operation.Provider != "shell" {
				kept = append(kept, operation)
			}
		}
		plan.Operations = kept
		plan.Skipped = append(plan.Skipped, model.Skipped{
			Provider: "shell",
			Resource: "shell:config",
			Reason:   fmt.Sprintf("required plugin(s) %s are not installed and cannot be restored in this plan; restore plugins first or run aggregate restore", strings.Join(blocked, ", ")),
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
