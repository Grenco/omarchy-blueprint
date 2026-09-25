package app

import (
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Each ChangeTargetKey below maps one provider's Diff changes onto the target
// keys its InspectTargets reports (see workflow.ChangeTargetResolver). A
// change kind a provider does not recognise returns ok=false so workflow
// treats the whole category conservatively instead of hiding a difference.

func (p restoreProviderAdapter) ChangeTargetKey(change model.Change) (string, bool) {
	resolver, ok := p.stateProvider.(workflow.ChangeTargetResolver)
	if !ok {
		return "", false
	}
	return resolver.ChangeTargetKey(change)
}

func (packagesStateProvider) ChangeTargetKey(change model.Change) (string, bool) {
	switch change.Kind {
	case "official", "aur", "mise", "preinstall":
		return change.Kind + ":" + change.Name, true
	case "preinstalls":
		return "preinstalls", true
	}
	return "", false
}

func (themesStateProvider) ChangeTargetKey(change model.Change) (string, bool) {
	switch change.Kind {
	case "theme":
		return "theme:" + change.Name, true
	case "active":
		return "active", true
	}
	return "", false
}

func (pluginsStateProvider) ChangeTargetKey(change model.Change) (string, bool) {
	if change.Kind == "plugin" {
		return "plugin:" + change.Name, true
	}
	return "", false
}

func (configStateProvider) ChangeTargetKey(change model.Change) (string, bool) {
	if change.Kind != "config" {
		return "", false
	}
	switch change.Name {
	case "exclusions", "inclusions":
		// Config management settings changed; no individual path did.
		return "", true
	}
	return change.Name, true
}

func (defaultsStateProvider) ChangeTargetKey(change model.Change) (string, bool) {
	if change.Kind == "default" {
		return change.Name, true
	}
	return "", false
}

// Shell has a single whole-category target.
func (shellStateProvider) ChangeTargetKey(change model.Change) (string, bool) {
	if change.Kind == "shell" {
		return "state", true
	}
	return "", false
}

func (hooksStateProvider) ChangeTargetKey(change model.Change) (string, bool) {
	if change.Kind == "hook" {
		return change.Name, true
	}
	return "", false
}

// Resource links are not policy targets, so a link change stays unattributed.
func (resourcesStateProvider) ChangeTargetKey(change model.Change) (string, bool) {
	if change.Kind == "resource" {
		return "resource:" + change.Name, true
	}
	return "", false
}

// Each RestoreOperationTargetKeys below maps one provider's Restore plan
// operations onto its InspectTargets keys (see
// workflow.RestoreTargetResolver). Unrecognised operations return ok=false,
// so workflow treats them conservatively.

func (p restoreProviderAdapter) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	resolver, ok := p.stateProvider.(workflow.RestoreTargetResolver)
	if !ok {
		return nil, false
	}
	return resolver.RestoreOperationTargetKeys(op)
}

// Package installs are batched per kind ("official:a,b"), so each listed
// item is its own target. The Mise guard only protects the declaration,
// and the declaration write changes every listed tool.
func (packagesStateProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	if op.Resource == "preinstalls" {
		return []string{"preinstalls"}, true
	}
	if op.Resource == "mise:global-tools" {
		if op.Action == "verify" {
			return nil, true
		}
		return prefixed("mise:", op.Items), op.Action == "configure"
	}
	for _, kind := range []string{"official", "aur", "mise", "preinstall"} {
		if !strings.HasPrefix(op.Resource, kind+":") {
			continue
		}
		if len(op.Items) == 0 {
			return []string{op.Resource}, true
		}
		return prefixed(kind+":", op.Items), true
	}
	return nil, false
}

func prefixed(prefix string, items []string) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, prefix+item)
	}
	return keys
}

func singleKey(key string, ok bool) ([]string, bool) {
	if !ok {
		return nil, false
	}
	return []string{key}, true
}

func (themesStateProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	if op.Action == "activate" {
		return []string{"active"}, true
	}
	return singleKey(op.Resource, strings.HasPrefix(op.Resource, "theme:"))
}

func (pluginsStateProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return singleKey(op.Resource, strings.HasPrefix(op.Resource, "plugin:"))
}

// Reloading Hyprland supports other Config operations; it targets no path.
func (configStateProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	if op.Action == "reload" {
		return nil, true
	}
	return singleKey(strings.CutPrefix(op.Resource, "config:"))
}

func (defaultsStateProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return singleKey(strings.CutPrefix(op.Resource, "default:"))
}

// Shell has a single whole-category target; restarting the shell only
// supports writing it.
func (shellStateProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	switch {
	case op.Action == "restart":
		return nil, true
	case strings.HasPrefix(op.Resource, "shell:"):
		return []string{"state"}, true
	}
	return nil, false
}

func (hooksStateProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return singleKey(strings.CutPrefix(op.Resource, "hook:"))
}

// Resource links are not policy targets, so a link operation stays
// unattributed.
func (resourcesStateProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return singleKey(op.Resource, strings.HasPrefix(op.Resource, "resource:"))
}
