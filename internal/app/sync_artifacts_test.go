package app

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"testing"
)

func TestSyncHistoricalArtifactsCannotBeInventedFromMetadata(t *testing.T) {
	root, deps := configSandbox(t)
	d := profile.Data{}
	d.Manifest.Capture = profile.CaptureMeta{Config: true, Hooks: true, Shell: true, Themes: true, Plugins: true, Resources: true, Services: true}
	d.Config.Files = []profile.ConfigFile{{Path: ".config/demo", Hash: "missing"}}
	d.Hooks.Items = []profile.Hook{{Path: "post-update.d/demo", Hash: "missing"}}
	d.Shell = profile.Shell{Hash: "missing", BaselineHash: "missing", Version: 1}
	d.Themes.Items = []profile.Theme{{ID: "demo", Type: "local", Hash: "missing"}}
	d.Plugins.Items = []profile.Plugin{{ID: "demo", Source: "local", Hash: "missing"}}
	d.Resources.Items = []profile.Resource{{ID: "demo", Path: "~/demo", Strategy: "copy", Kind: "directory", Hash: "missing", Mode: "0755"}}
	d.Services.Units = []profile.ServiceUnit{{Name: "demo.service", Presence: profile.ServicePresent, Management: profile.ServiceManagementDefinition, Definition: "units/demo.service", DefinitionHash: "missing"}}
	for _, p := range workflowProviders(deps, &options{profileDir: root}) {
		if p.ID() == "packages" || p.ID() == "defaults" {
			continue
		}
		validator, ok := p.(interface {
			ValidateSyncArtifacts(context.Context, string, profile.Data) error
		})
		if !ok {
			t.Fatalf("%s lacks historical artifact validation", p.ID())
		}
		if err := validator.ValidateSyncArtifacts(context.Background(), root, d); err == nil {
			t.Fatalf("%s trusted missing artifacts", p.ID())
		}
	}
}
