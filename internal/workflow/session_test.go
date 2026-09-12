package workflow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestWorkflowOpenCanonicalizesProfileAndResolvesMachine(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{Name: "desktop"}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: filepath.Join(profileDir, "."), ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	if session.ProfileDir() != profileDir {
		t.Errorf("profile dir = %q, want %q", session.ProfileDir(), profileDir)
	}
	if session.Machine().Name != "desktop" || session.Machine().Source != "explicit" {
		t.Errorf("machine = %#v", session.Machine())
	}
}

func TestSessionSetConfigPolicySavesAndReloads(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Config.Included = []string{".config/nvim", ".config/nvim/lua"}
	data.Config.Excluded = []string{".config/nvim/cache"}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }, Now: func() time.Time { return time.Unix(1, 0) }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetConfigPolicy(context.Background(), ".config/nvim", "auto"); err != nil {
		t.Fatal(err)
	}
	if got := session.Profile().Config.Included; len(got) != 1 || got[0] != ".config/nvim/lua" {
		t.Fatalf("included=%v", got)
	}
	if err := session.SetConfigPolicy(context.Background(), ".config/nvim", "invalid"); err == nil {
		t.Fatal("invalid policy accepted")
	}
}

func TestSessionSetProviderCapturedSavesOptionalProvider(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetProviderCaptured(context.Background(), "themes", true); err != nil {
		t.Fatal(err)
	}
	if !session.Profile().Manifest.Capture.Themes {
		t.Fatal("themes were not enabled")
	}
	loaded, err := profile.Load(profileDir)
	if err != nil || !loaded.Manifest.Capture.Themes {
		t.Fatalf("saved capture state=%#v err=%v", loaded.Manifest.Capture, err)
	}
	if err := session.SetProviderCaptured(context.Background(), "packages", false); err == nil {
		t.Fatal("packages accepted optional toggle")
	}
}

func TestSessionReloadRepairsBareLegacyPackageExclusion(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Packages.Excluded = []string{"1password", "aur:valid-package"}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if got := session.Profile().Packages.Excluded; len(got) != 1 || got[0] != "aur:valid-package" {
		t.Fatalf("excluded=%v", got)
	}
}

func TestSessionPersistsThemeAndPluginBlueprintExclusions(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Themes.Items = []profile.Theme{{ID: "nord", Enabled: true}}
	data.Plugins.Items = []profile.Plugin{{ID: "clock", Source: "builtin", Enabled: false}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetProviderItemEnabled(context.Background(), "themes", "Saved themes", "nord"); err != nil {
		t.Fatal(err)
	}
	if err := session.SetProviderItemEnabled(context.Background(), "plugins", "Built-in plugins", "clock"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Profile().Themes.Excluded) != 1 || reopened.Profile().Themes.Excluded[0] != "nord" {
		t.Fatalf("themes=%v", reopened.Profile().Themes.Excluded)
	}
	if len(reopened.Profile().Plugins.Excluded) != 1 || reopened.Profile().Plugins.Excluded[0] != "clock" {
		t.Fatalf("plugins=%v", reopened.Profile().Plugins.Excluded)
	}
	if reopened.Profile().Plugins.Items[0].Enabled {
		t.Fatal("runtime plugin enabled state changed")
	}
}
