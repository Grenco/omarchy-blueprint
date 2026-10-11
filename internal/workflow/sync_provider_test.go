package workflow_test

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/providers/config"
	"github.com/Grenco/omarchy-blueprint/internal/providers/defaults"
	"github.com/Grenco/omarchy-blueprint/internal/providers/hooks"
	"github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	"github.com/Grenco/omarchy-blueprint/internal/providers/plugins"
	"github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/providers/services"
	"github.com/Grenco/omarchy-blueprint/internal/providers/shell"
	"github.com/Grenco/omarchy-blueprint/internal/providers/themes"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type syncOverlay interface {
	SyncDesired(profile.Data) ([]syncengine.DesiredTarget, error)
	OverlaySyncTarget(context.Context, *syncprofile.Builder, syncprofile.View, string) error
}

func TestSyncConfigDeletionOverlayRetainsBaselineEvidence(t *testing.T) {
	src := syncprofile.View{Root: t.TempDir(), Profile: profile.New("test", time.Time{})}
	src.Profile.Manifest.Capture.Config = true
	src.Profile.Config.Deletes = []profile.ConfigDelete{{Path: ".config/demo", BaselineHash: "baseline"}}
	artifact := "config/baseline/.config/demo"
	path := filepath.Join(src.Root, artifact)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("baseline evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := syncprofile.NewBuilder(profile.New("test", time.Time{}))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := (config.Provider{}).OverlaySyncTarget(context.Background(), b, src, ".config/demo"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(b.Root(), artifact))
	if err != nil || string(got) != "baseline evidence" {
		t.Fatal("deletion lost baseline authority evidence", err)
	}
}

func TestSyncProviderOverlayOneTargetAndUnmanaged(t *testing.T) {
	cases := []struct {
		p         syncOverlay
		target    string
		setup     func(*profile.Data)
		artifacts []string
	}{
		{packages.Provider{}, "official:demo", func(d *profile.Data) { d.Manifest.Capture.Packages = true; d.Packages.Official = []string{"demo"} }, nil},
		{config.Provider{}, ".config/demo", func(d *profile.Data) {
			d.Manifest.Capture.Config = true
			d.Config.Files = []profile.ConfigFile{{Path: ".config/demo", Hash: "hash"}}
		}, []string{"config/files/.config/demo"}},
		{hooks.Provider{}, "post-update.d/demo", func(d *profile.Data) {
			d.Manifest.Capture.Hooks = true
			d.Hooks.Items = []profile.Hook{{Path: "post-update.d/demo", Hash: "hash", Mode: "0755"}}
		}, []string{"hooks/files/post-update.d/demo"}},
		{shell.Provider{}, "state", func(d *profile.Data) {
			d.Manifest.Capture.Shell = true
			d.Shell = profile.Shell{Hash: "hash", BaselineHash: "base", Version: 1}
		}, []string{"shell/shell.json", "shell/baseline.json"}},
		{&services.Provider{}, "demo.service", func(d *profile.Data) {
			d.Manifest.Capture.Services = true
			d.Services.Units = []profile.ServiceUnit{{Name: "demo.service", Kind: "service", Management: profile.ServiceManagementDefinition, Presence: profile.ServicePresent, StartIntent: profile.ServiceStartDisabled, Definition: "units/demo.service", DefinitionHash: "hash"}}
		}, []string{"services/units/demo.service"}},
		{themes.Provider{}, "theme:demo", func(d *profile.Data) {
			d.Manifest.Capture.Themes = true
			d.Themes.Items = []profile.Theme{{ID: "demo", Type: "local", Hash: "hash"}}
		}, []string{"themes/local/demo/content"}},
		{plugins.Provider{}, "plugin:demo", func(d *profile.Data) {
			d.Manifest.Capture.Plugins = true
			d.Plugins.Items = []profile.Plugin{{ID: "demo", Source: "local", Hash: "hash"}}
		}, []string{"plugins/local/demo/content"}},
		{defaults.Provider{}, "browser", func(d *profile.Data) { d.Manifest.Capture.Defaults = true; d.Defaults.Browser = "firefox" }, nil},
		{resources.Provider{}, "resource:demo", func(d *profile.Data) {
			d.Manifest.Capture.Resources = true
			d.Resources.Items = []profile.Resource{{ID: "demo", Path: "~/demo", Kind: "directory", Strategy: "copy", Hash: "hash"}}
		}, []string{"resources/files/demo/content"}},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			src := syncprofile.View{Root: t.TempDir(), Profile: profile.New("test", time.Time{})}
			c.setup(&src.Profile)
			for _, rel := range c.artifacts {
				path := filepath.Join(src.Root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("chosen"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			original := profile.New("test", time.Time{})
			original.Manifest.Capture.Defaults = true
			original.Defaults.Editor = "nvim"
			b, err := syncprofile.NewBuilder(original)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			if err := c.p.OverlaySyncTarget(context.Background(), b, src, c.target); err != nil {
				t.Fatal(err)
			}
			if b.Profile.Defaults.Editor != "nvim" {
				t.Fatal("unrelated desired target changed")
			}
			want, _ := c.p.SyncDesired(src.Profile)
			got, _ := c.p.SyncDesired(b.Profile)
			found := false
			for _, x := range got {
				if x.Key.Target == c.target {
					for _, w := range want {
						if w.Key == x.Key && w.State.Equal(x.State) {
							found = true
						}
					}
				}
			}
			if !found {
				t.Fatal("chosen semantic target missing", got, want)
			}
			for _, rel := range c.artifacts {
				data, err := os.ReadFile(filepath.Join(b.Root(), filepath.FromSlash(rel)))
				if err != nil || string(data) != "chosen" {
					t.Fatal("artifact missing", rel, err)
				}
			}
			if err := profile.Save(b.Root(), b.Profile); err != nil {
				t.Fatal(err)
			}
			if _, err := profile.Load(b.Root()); err != nil {
				t.Fatal(err)
			}
			stale := src
			stale.Profile = profile.CloneData(src.Profile)
			stale.Profile.Manifest.Capture = profile.CaptureMeta{}
			// Packages predate capture metadata: stored declarations remain
			// managed even when the manifest flag is absent. Empty is unmanaged.
			stale.Profile.Packages = profile.Packages{}
			if err := c.p.OverlaySyncTarget(context.Background(), b, stale, c.target); err != nil {
				t.Fatal(err)
			}
			stopped, err := c.p.SyncDesired(b.Profile)
			if err != nil {
				t.Fatal(err)
			}
			for _, x := range stopped {
				if x.Key.Target == c.target {
					t.Fatal("uncaptured stale metadata adopted", x)
				}
			}
			empty := syncprofile.View{Root: t.TempDir(), Profile: original}
			if err := c.p.OverlaySyncTarget(context.Background(), b, empty, c.target); err != nil {
				t.Fatal(err)
			}
			got, err = c.p.SyncDesired(b.Profile)
			if err != nil {
				t.Fatal(err)
			}
			for _, x := range got {
				if x.Key.Target == c.target {
					t.Fatal("unmanaged became absent/present", x)
				}
			}
			for _, rel := range c.artifacts {
				if _, err := os.Stat(filepath.Join(b.Root(), filepath.FromSlash(rel))); !os.IsNotExist(err) {
					t.Fatal("losing artifact retained", rel, err)
				}
			}
		})
	}
}
