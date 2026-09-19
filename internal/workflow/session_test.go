package workflow

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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

// TestSessionOpenRejectsInvalidLegacyPackageExclusionWithoutMutation is
// updated for the profile-layer cutover: Packages.Excluded is no longer
// persisted by Save at all (Session.SetPackageExcluded is the sole write
// path now), so a malformed exclusion can only be observed by Load through
// a stray packages/excluded.txt file, exactly like a legacy pre-schema-12
// profile. migrateLegacyPackageExclusions rejects a malformed ref rather
// than silently turning it into a garbage policy target.
func TestSessionOpenRejectsInvalidLegacyPackageExclusionWithoutMutation(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "packages", "excluded.txt"), []byte("1password\naur:valid-package\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(profileDir, "profile.toml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir}); err == nil || !strings.Contains(err.Error(), "migrate legacy package exclusions") {
		t.Fatalf("err=%v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("reload mutated profile.toml")
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

// TestReloadRevalidatesPolicyAgainstAlreadyRegisteredProviders is a
// regression for a round-3 review finding on PR 3: SetProviders validates
// loaded policy targets when providers are first registered, but Reload
// (which ProfileGitPull also calls after fetching remote changes) did not
// revalidate the freshly loaded candidate profile against that
// already-installed registry. So an already-open session could reload a
// profile containing a malformed hand-edited policy target and silently
// accept it, even though opening that same profile fresh would fail in
// SetProviders.
func TestReloadRevalidatesPolicyAgainstAlreadyRegisteredProviders(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	// Hand-edit the on-disk policy after the session is already open and
	// providers are already registered, exactly like an external `git pull`
	// landing a bad profile underneath a running session.
	if err := os.MkdirAll(filepath.Join(session.ProfileDir(), "policy"), 0o755); err != nil {
		t.Fatal(err)
	}
	policyToml := "capture = [{category = \"packages\", target = \"has space\", setting = \"disabled\"}]\n"
	if err := os.WriteFile(filepath.Join(session.ProfileDir(), "policy", "policy.toml"), []byte(policyToml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := session.Reload(); err == nil {
		t.Fatal("hand-edited invalid policy target silently accepted on reload")
	}
	if got := session.Profile().Policy; len(got.Capture) != 0 {
		t.Fatalf("policy = %+v, want the session's in-memory profile left untouched", got)
	}
}

// TestProfileGitPullRejectsInvalidPolicyFromRemote mirrors
// TestReloadRevalidatesPolicyAgainstAlreadyRegisteredProviders via the real
// ProfileGitPull path the review comment called out by name: a remote
// change that lands a malformed hand-edited policy target must fail the
// pull's implicit Reload rather than silently updating the running
// session's profile.
func TestProfileGitPullRejectsInvalidPolicyFromRemote(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(other, "policy"), 0o755); err != nil {
		t.Fatal(err)
	}
	policyToml := "capture = [{category = \"packages\", target = \"has space\", setting = \"disabled\"}]\n"
	if err := os.WriteFile(filepath.Join(other, "policy", "policy.toml"), []byte(policyToml), 0o644); err != nil {
		t.Fatal(err)
	}
	run("-C", other, "config", "user.name", "Blueprint Test")
	run("-C", other, "config", "user.email", "blueprint@example.test")
	run("-C", other, "add", ".")
	run("-C", other, "commit", "-m", "remote change with a bad hand-edited policy target")
	run("-C", other, "push")

	session, err := Open(Dependencies{Runner: runner, StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: root})
	if err != nil {
		t.Fatal(err)
	}
	session.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}}})
	if _, err := session.ProfileGitFetch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ProfileGitPull(ctx); err == nil {
		t.Fatal("pull with a malformed hand-edited policy target silently accepted")
	}
	if got := session.Profile().Manifest.Profile.Name; got != "local" {
		t.Fatalf("profile after rejected pull=%q, want the session's in-memory profile left untouched", got)
	}
}
