package workflow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
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

func TestSessionProfileGitPullReloadsProfile(t *testing.T) {
	ctx := context.Background()
	root, stateHome, remote, other := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "origin.git"), filepath.Join(t.TempDir(), "other")
	data := profile.New("local", time.Now())
	if err := profile.Save(root, data); err != nil {
		t.Fatal(err)
	}
	runner := command.SystemRunner{}
	run := func(args ...string) {
		t.Helper()
		if _, err := runner.Run(ctx, "git", args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init", "--bare", remote)
	run("-C", root, "init", "-b", "main")
	run("-C", root, "config", "user.name", "Blueprint Test")
	run("-C", root, "config", "user.email", "blueprint@example.test")
	run("-C", root, "add", ".")
	run("-C", root, "commit", "-m", "initial")
	run("-C", root, "remote", "add", "origin", remote)
	run("-C", root, "push", "-u", "origin", "main")
	run("clone", "--branch", "main", remote, other)
	remoteData, err := profile.Load(other)
	if err != nil {
		t.Fatal(err)
	}
	remoteData.Manifest.Profile.Name = "remote"
	if err := profile.Save(other, remoteData); err != nil {
		t.Fatal(err)
	}
	run("-C", other, "config", "user.name", "Blueprint Test")
	run("-C", other, "config", "user.email", "blueprint@example.test")
	run("-C", other, "add", ".")
	run("-C", other, "commit", "-m", "remote change")
	run("-C", other, "push")

	session, err := Open(Dependencies{Runner: runner, StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ProfileGitFetch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ProfileGitPull(ctx); err != nil {
		t.Fatal(err)
	}
	if session.Profile().Manifest.Profile.Name != "remote" {
		t.Fatalf("profile after pull=%q", session.Profile().Manifest.Profile.Name)
	}
}
