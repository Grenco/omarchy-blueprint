package app

import (
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Each provider must attribute its own Diff changes to the exact target key
// its InspectTargets reports, or deliberately decline (ok=false) so
// workflow stays conservative.
func TestProvidersAttributeDiffChangesToTargetKeys(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider stateProvider
		change   model.Change
		key      string
		ok       bool
	}{
		{"official package", packagesStateProvider{}, model.Change{Kind: "official", Name: "firefox"}, "official:firefox", true},
		{"aur package", packagesStateProvider{}, model.Change{Kind: "aur", Name: "yay"}, "aur:yay", true},
		{"mise tool", packagesStateProvider{}, model.Change{Kind: "mise", Name: "node"}, "mise:node", true},
		{"omarchy preinstall", packagesStateProvider{}, model.Change{Kind: "preinstall", Name: "obsidian"}, "preinstall:obsidian", true},
		{"omarchy preinstall group", packagesStateProvider{}, model.Change{Kind: "preinstalls", Name: "preinstalls"}, "preinstalls", true},
		{"unknown package kind", packagesStateProvider{}, model.Change{Kind: "flatpak", Name: "x"}, "", false},
		{"theme", &themesStateProvider{}, model.Change{Kind: "theme", Name: "nord"}, "theme:nord", true},
		{"active theme", &themesStateProvider{}, model.Change{Kind: "active", Name: "nord"}, "active", true},
		{"plugin", &pluginsStateProvider{}, model.Change{Kind: "plugin", Name: "weather"}, "plugin:weather", true},
		{"config path", configStateProvider{}, model.Change{Kind: "config", Name: ".config/hypr/bindings.lua"}, ".config/hypr/bindings.lua", true},
		{"config exclusions setting", configStateProvider{}, model.Change{Kind: "config", Name: "exclusions"}, "", true},
		{"config inclusions setting", configStateProvider{}, model.Change{Kind: "config", Name: "inclusions"}, "", true},
		{"default application", defaultsStateProvider{}, model.Change{Kind: "default", Name: "browser"}, "browser", true},
		{"shell", shellStateProvider{}, model.Change{Kind: "shell", Name: "shell"}, "state", true},
		{"hook", hooksStateProvider{}, model.Change{Kind: "hook", Name: "post-update.d/setup.hook"}, "post-update.d/setup.hook", true},
		{"resource", &resourcesStateProvider{}, model.Change{Kind: "resource", Name: "projects"}, "resource:projects", true},
		{"resource link has no policy target", &resourcesStateProvider{}, model.Change{Kind: "link", Name: "link:x"}, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver, ok := restoreProviderAdapter{stateProvider: test.provider}.stateProvider.(workflow.ChangeTargetResolver)
			if !ok {
				t.Fatalf("%T does not attribute Diff changes to targets", test.provider)
			}
			key, attributed := resolver.ChangeTargetKey(test.change)
			if key != test.key || attributed != test.ok {
				t.Fatalf("ChangeTargetKey(%+v) = (%q, %v), want (%q, %v)", test.change, key, attributed, test.key, test.ok)
			}
		})
	}
}

func TestRestoreProviderAdapterForwardsChangeAttribution(t *testing.T) {
	var adapter workflow.Provider = restoreProviderAdapter{stateProvider: packagesStateProvider{}}
	resolver, ok := adapter.(workflow.ChangeTargetResolver)
	if !ok {
		t.Fatal("workflow sees providers through restoreProviderAdapter, which must forward change attribution")
	}
	if key, attributed := resolver.ChangeTargetKey(model.Change{Kind: "official", Name: "git"}); key != "official:git" || !attributed {
		t.Fatalf("adapter attribution = (%q, %v)", key, attributed)
	}
}

// Categories the identity contract sandbox cannot drive into Restore
// operations are pinned here against their operation formats.
func TestProvidersAttributeRestoreOperationsToTargetKeys(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider stateProvider
		op       model.Operation
		keys     []string
		ok       bool
	}{
		{"batched official install", packagesStateProvider{}, model.Operation{Action: "install", Resource: "official:bat,ripgrep", Items: []string{"bat", "ripgrep"}}, []string{"official:bat", "official:ripgrep"}, true},
		{"batched mise install", packagesStateProvider{}, model.Operation{Action: "install", Resource: "mise:node,go", Items: []string{"node", "go"}}, []string{"mise:node", "mise:go"}, true},
		{"mise declaration write", packagesStateProvider{}, model.Operation{Action: "configure", Resource: "mise:global-tools", Items: []string{"node"}}, []string{"mise:node"}, true},
		{"mise declaration guard", packagesStateProvider{}, model.Operation{Action: "verify", Resource: "mise:global-tools"}, nil, true},
		{"package removal by absence ref", packagesStateProvider{}, model.Operation{Action: "remove", Resource: "mise:node", Items: []string{"node"}}, []string{"mise:node"}, true},
		{"preinstall group", packagesStateProvider{}, model.Operation{Action: "install", Resource: "preinstalls"}, []string{"preinstalls"}, true},
		{"preinstall item", packagesStateProvider{}, model.Operation{Action: "remove", Resource: "preinstall:obsidian", Items: []string{"obsidian"}}, []string{"preinstall:obsidian"}, true},
		{"theme install", &themesStateProvider{}, model.Operation{Action: "install", Resource: "theme:nord"}, []string{"theme:nord"}, true},
		{"theme activation", &themesStateProvider{}, model.Operation{Action: "activate", Resource: "theme:nord"}, []string{"active"}, true},
		{"plugin", &pluginsStateProvider{}, model.Operation{Resource: "plugin:weather"}, []string{"plugin:weather"}, true},
		{"default application", defaultsStateProvider{}, model.Operation{Resource: "default:browser"}, []string{"browser"}, true},
		{"shell write", shellStateProvider{}, model.Operation{Action: "write", Resource: "shell:config"}, []string{"state"}, true},
		{"shell restart supports the write", shellStateProvider{}, model.Operation{Action: "restart", Resource: "shell:runtime"}, nil, true},
		{"config reload supports the writes", configStateProvider{}, model.Operation{Action: "reload", Resource: "config:hyprctl"}, nil, true},
		{"resource", &resourcesStateProvider{}, model.Operation{Resource: "resource:projects"}, []string{"resource:projects"}, true},
		{"resource link has no policy target", &resourcesStateProvider{}, model.Operation{Resource: "link:x"}, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver, ok := test.provider.(workflow.RestoreTargetResolver)
			if !ok {
				t.Fatalf("%T does not attribute Restore operations", test.provider)
			}
			keys, attributed := resolver.RestoreOperationTargetKeys(test.op)
			if !reflect.DeepEqual(keys, test.keys) || attributed != test.ok {
				t.Fatalf("RestoreOperationTargetKeys(%+v) = (%q, %v), want (%q, %v)", test.op, keys, attributed, test.keys, test.ok)
			}
		})
	}
}
