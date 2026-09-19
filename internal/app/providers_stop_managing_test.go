package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestPackagesStopManagingRemovesDesiredPresentState(t *testing.T) {
	d := profile.Data{Packages: profile.Packages{Official: []string{"firefox", "zoxide"}}}
	got, err := (packagesStateProvider{}).StopManaging(context.Background(), d, "official:firefox")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages.Official) != 1 || got.Packages.Official[0] != "zoxide" {
		t.Fatalf("official = %#v, want firefox removed", got.Packages.Official)
	}
	// The session's own copy must never be mutated by the provider's return.
	if len(d.Packages.Official) != 2 {
		t.Fatalf("caller's data mutated: %#v", d.Packages.Official)
	}
}

func TestPackagesStopManagingRemovesDesiredAbsentState(t *testing.T) {
	d := profile.Data{Packages: profile.Packages{Absent: []profile.PackageAbsence{{Ref: "mise:node"}}}}
	got, err := (packagesStateProvider{}).StopManaging(context.Background(), d, "mise:node")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages.Absent) != 0 {
		t.Fatalf("absent = %#v, want cleared", got.Packages.Absent)
	}
}

func TestPackagesStopManagingUnmanagedTargetIsError(t *testing.T) {
	if _, err := (packagesStateProvider{}).StopManaging(context.Background(), profile.Data{}, "official:firefox"); err == nil {
		t.Fatal("unmanaged target accepted")
	}
}

func TestThemesStopManagingRemovesItemAndArtifact(t *testing.T) {
	profileDir := t.TempDir()
	artifact := filepath.Join(profileDir, "themes", "local", "nord")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifact, "colors.toml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := profile.Data{Themes: profile.Themes{Items: []profile.Theme{{ID: "nord", Type: "local"}}}}
	p := themesStateProvider{opt: &options{profileDir: profileDir}}
	got, err := p.StopManaging(context.Background(), d, "theme:nord")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Themes.Items) != 0 {
		t.Fatalf("items = %#v, want removed", got.Themes.Items)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatal("theme artifact not removed")
	}
}

func TestThemesStopManagingRejectsBuiltinTheme(t *testing.T) {
	d := profile.Data{Themes: profile.Themes{Items: []profile.Theme{{ID: "nord", Type: "builtin"}}}}
	p := themesStateProvider{opt: &options{profileDir: t.TempDir()}}
	if _, err := p.StopManaging(context.Background(), d, "theme:nord"); err == nil {
		t.Fatal("built-in theme accepted")
	}
}

func TestThemesStopManagingRejectsActiveTarget(t *testing.T) {
	p := themesStateProvider{opt: &options{profileDir: t.TempDir()}}
	if _, err := p.StopManaging(context.Background(), profile.Data{}, "active"); err == nil {
		t.Fatal("active target accepted")
	}
}

func TestPluginsStopManagingRemovesItemAndArtifact(t *testing.T) {
	profileDir := t.TempDir()
	artifact := filepath.Join(profileDir, "plugins", "local", "acme")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	d := profile.Data{Plugins: profile.Plugins{Items: []profile.Plugin{{ID: "acme", Source: "git"}}}}
	p := pluginsStateProvider{opt: &options{profileDir: profileDir}}
	got, err := p.StopManaging(context.Background(), d, "plugin:acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Plugins.Items) != 0 {
		t.Fatalf("items = %#v, want removed", got.Plugins.Items)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatal("plugin artifact not removed")
	}
}

func TestPluginsStopManagingRejectsBuiltinPlugin(t *testing.T) {
	d := profile.Data{Plugins: profile.Plugins{Items: []profile.Plugin{{ID: "acme", Source: "builtin"}}}}
	p := pluginsStateProvider{opt: &options{profileDir: t.TempDir()}}
	if _, err := p.StopManaging(context.Background(), d, "plugin:acme"); err == nil {
		t.Fatal("built-in plugin accepted")
	}
}

func TestHooksStopManagingRemovesItemAndArtifact(t *testing.T) {
	profileDir := t.TempDir()
	artifact := filepath.Join(profileDir, "hooks", "files", "post-boot")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := profile.Data{Hooks: profile.Hooks{Items: []profile.Hook{{Path: "post-boot", Hash: strings.Repeat("a", 64), Mode: "0644"}}}}
	p := hooksStateProvider{opt: &options{profileDir: profileDir}}
	got, err := p.StopManaging(context.Background(), d, "post-boot")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Hooks.Items) != 0 {
		t.Fatalf("items = %#v, want removed", got.Hooks.Items)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatal("hook artifact not removed")
	}
}

func TestHooksStopManagingUnmanagedTargetIsError(t *testing.T) {
	p := hooksStateProvider{opt: &options{profileDir: t.TempDir()}}
	if _, err := p.StopManaging(context.Background(), profile.Data{}, "post-boot"); err == nil {
		t.Fatal("unmanaged hook accepted")
	}
}

func TestDefaultsStopManagingClearsSlot(t *testing.T) {
	d := profile.Data{Defaults: profile.Defaults{Terminal: "ghostty", Browser: "firefox"}}
	got, err := (defaultsStateProvider{}).StopManaging(context.Background(), d, "terminal")
	if err != nil {
		t.Fatal(err)
	}
	if got.Defaults.Terminal != "" || got.Defaults.Browser != "firefox" {
		t.Fatalf("defaults = %#v, want only terminal cleared", got.Defaults)
	}
}

func TestDefaultsStopManagingUnmanagedSlotIsError(t *testing.T) {
	if _, err := (defaultsStateProvider{}).StopManaging(context.Background(), profile.Data{}, "terminal"); err == nil {
		t.Fatal("unmanaged slot accepted")
	}
}

func TestConfigStopManagingIsRejected(t *testing.T) {
	if _, err := (configStateProvider{}).StopManaging(context.Background(), profile.Data{}, ".config/nvim/init.lua"); err == nil {
		t.Fatal("config Stop Managing accepted")
	}
}

func TestResourcesStopManagingIsRejectedAndMentionsUntrack(t *testing.T) {
	_, err := (resourcesStateProvider{}).StopManaging(context.Background(), profile.Data{}, "resource:dotfiles")
	if err == nil {
		t.Fatal("resources Stop Managing accepted")
	}
	if !strings.Contains(err.Error(), "Untrack") {
		t.Fatalf("err = %v, want it to direct callers to Untrack", err)
	}
}

func TestShellStopManagingIsRejected(t *testing.T) {
	if _, err := (shellStateProvider{}).StopManaging(context.Background(), profile.Data{}, "state"); err == nil {
		t.Fatal("shell Stop Managing accepted")
	}
}

// TestSetPolicyRejectsMalformedTargetThroughRealWorkflowSession proves
// ValidateTarget is reached the same way through the real adapter chain
// openWorkflow builds, not just a direct unit call on the bare provider.
func TestSetPolicyRejectsMalformedTargetThroughRealWorkflowSession(t *testing.T) {
	profileDir, deps := configSandbox(t)
	session, err := openWorkflow(deps, &options{profileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetPolicy(workflow.PolicyScope{}, policy.AxisCapture, "packages", "not-a-valid-target", policy.SettingDisabled); err == nil {
		t.Fatal("malformed target accepted through the real workflow session")
	}
}

// TestStopManagingReachesRealProviderThroughWorkflowSession is a regression
// for a review finding on PR 3: restoreProviderAdapter embeds the
// stateProvider INTERFACE, which does not declare StopManaging, so a real
// workflow.Session built by openWorkflow could not reach a wrapped
// provider's StopManaging even though direct provider unit tests passed.
// This exercises the real adapter chain end to end, not a fake.
func TestStopManagingReachesRealProviderThroughWorkflowSession(t *testing.T) {
	profileDir, deps := configSandbox(t)
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	d.Packages.Official = []string{"firefox"}
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	session, err := openWorkflow(deps, &options{profileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.StopManaging(context.Background(), "packages", "official:firefox"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Packages.Official) != 0 {
		t.Fatalf("official = %#v, want firefox removed through the real wrapped provider", reloaded.Packages.Official)
	}
}
