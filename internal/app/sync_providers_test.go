package app

import (
	"context"
	"path/filepath"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"testing"
)

func TestSyncProjectionTargetIdentityContract(t *testing.T) {
	dir, deps := configSandbox(t)
	data := profile.Data{}
	data.Manifest.Capture = profile.CaptureMeta{Packages: true, Themes: true, Plugins: true, Config: true, Defaults: true, Shell: true, Hooks: true, Services: true, Resources: true}
	data.Packages = profile.Packages{Official: []string{"demo"}, Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{"firefox": false}}}
	data.Themes = profile.Themes{Current: "builtin", Items: []profile.Theme{{ID: "demo", Type: "local", Hash: "hash"}}}
	data.Plugins.Items = []profile.Plugin{{ID: "demo", Source: "builtin"}}
	data.Config.Files = []profile.ConfigFile{{Path: ".config/demo", Hash: "hash"}}
	data.Defaults.Browser = "firefox"
	data.Shell = profile.Shell{Hash: "hash", BaselineHash: "base", Version: 1}
	data.Hooks.Items = []profile.Hook{{Path: "post-update.d/demo", Hash: "hash"}}
	data.Services.Units = []profile.ServiceUnit{{Name: "demo.service", Presence: profile.ServicePresent}}
	data.Resources.Items = []profile.Resource{{ID: "demo", Path: "~/demo", Strategy: "copy", Hash: "hash"}}
	for _, p := range workflowProviders(deps, &options{profileDir: dir}) {
		projection, ok := p.(workflow.SyncDesiredProvider)
		if !ok {
			t.Fatalf("%s lacks sync projection", p.ID())
		}
		got, err := projection.SyncDesired(data)
		if err != nil || len(got) == 0 {
			t.Fatalf("%s %v %v", p.ID(), got, err)
		}
		validator, ok := p.(interface{ ValidateTarget(string) (string, error) })
		if !ok {
			t.Fatal(p.ID())
		}
		for _, x := range got {
			if x.Key.Category != p.ID() {
				t.Fatal(x.Key)
			}
			valid, err := validator.ValidateTarget(x.Key.Target)
			if err != nil || valid != x.Key.Target {
				t.Fatalf("%s invalid policy identity %s", p.ID(), x.Key.Target)
			}
		}
	}
}

// Existing policy inspection and Sync must share identities, not invent a
// filename-derived namespace for the same live/desired target.
func TestSyncProjectionMatchesInspectTargets(t *testing.T) {
	dir, deps := configSandbox(t)
	deps.MiseGlobalConfig=func()(string,error){return filepath.Join(t.TempDir(),"mise.toml"),nil}
	d := profile.Data{}
	d.Manifest.Capture.Packages = true
	d.Packages.Official = []string{"ripgrep"}
	p := workflowProviders(deps, &options{profileDir: dir})[0]
	desired, err := p.(workflow.SyncDesiredProvider).SyncDesired(d)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := p.InspectTargets(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range desired {
		found := false
		for _, o := range observed {
			if x.Key.Target == o.Key && x.State.Kind == syncengine.StatePresent {
				found = true
			}
		}
		if !found {
			t.Fatal(x)
		}
	}
}
