package app

import (
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
