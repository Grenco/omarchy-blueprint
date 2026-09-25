package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Each case changes only part of what Capture persists for a target while
// its outcome stays the same. The approval contract must still see it: the
// fingerprint has to cover the whole prospective value, not chosen fields.

func fingerprintSession(t *testing.T, deps Dependencies, profileDir string) *workflow.Session {
	t.Helper()
	// Keep providers in the sandbox rather than Execute's real defaults.
	if deps.HomeDir == nil {
		_, userRoot, err := deps.ConfigDirs()
		if err != nil {
			t.Fatal(err)
		}
		home := filepath.Dir(userRoot)
		deps.HomeDir = func() (string, error) { return home, nil }
	}
	pluginDir, miseConfig := t.TempDir(), filepath.Join(t.TempDir(), "mise.toml")
	deps.PluginDir = func() (string, error) { return pluginDir, nil }
	deps.MiseGlobalConfig = func() (string, error) { return miseConfig, nil }
	deps.Hostname = func() (string, error) { return "sandbox", nil }
	if deps.ResourceLinkRoots == nil {
		deps.ResourceLinkRoots = func(string) []resourcesprovider.LinkSearchRoot { return nil }
	}
	opt := &options{profileDir: profileDir}
	session, err := workflow.Open(workflowDependencies(deps), workflow.Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetProviders(workflowProviders(deps, opt)); err != nil {
		t.Fatal(err)
	}
	return session
}

func reviewTarget(t *testing.T, session *workflow.Session, category, key string) (workflow.CaptureInspection, workflow.CaptureTarget) {
	t.Helper()
	inspection, err := session.InspectCaptureMany(context.Background(), []string{category})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range inspection.Categories[category] {
		if target.Inspection.Key == key {
			return inspection, target
		}
	}
	t.Fatalf("%s has no target %q: %#v", category, key, inspection.Categories[category])
	return workflow.CaptureInspection{}, workflow.CaptureTarget{}
}

// assertMaterialChange checks the outcome held but the approval would
// still be refused.
func assertMaterialChange(t *testing.T, session *workflow.Session, category, key string, approved workflow.CaptureInspection, before workflow.CaptureTarget, want workflow.CaptureOutcome) {
	t.Helper()
	if before.Outcome != want {
		t.Fatalf("before: %s outcome = %s, want %s", key, before.Outcome, want)
	}
	fresh, after := reviewTarget(t, session, category, key)
	if after.Outcome != want {
		t.Fatalf("after: %s outcome = %s, want it unchanged at %s", key, after.Outcome, want)
	}
	changes := approved.ChangesFrom(fresh)
	if len(changes) == 0 {
		t.Fatalf("%s: what Capture persists changed under the same %s outcome, but the approval still matches (fingerprint %q)", key, want, after.Inspection.Fingerprint)
	}
}

func resourceSandbox(t *testing.T) (string, Dependencies, string) {
	t.Helper()
	profileDir, deps := configSandbox(t)
	home := t.TempDir()
	deps.HomeDir = func() (string, error) { return home, nil }
	deps.ResourceLinkRoots = func(string) []resourcesprovider.LinkSearchRoot { return nil }
	deps.Runner = &gitMachineRunner{machineRunner: machineRunner{official: map[string]bool{}, aur: map[string]bool{}}}
	return profileDir, deps, home
}

// The copy snapshot hash happens to include modes today; this guards the
// property itself, since the fingerprint now covers the whole entry.
func TestCaptureFingerprintCoversCopyResourceMode(t *testing.T) {
	profileDir, deps, home := resourceSandbox(t)
	file := filepath.Join(home, "todo.md")
	writeAppFile(t, file, "one\n")
	if code, out := configRun(t, deps, profileDir, "track", file); code != 0 {
		t.Fatalf("track code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "resources"); code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	writeAppFile(t, file, "two\n")
	session := fingerprintSession(t, deps, profileDir)
	id := session.Profile().Resources.Items[0].ID
	approved, before := reviewTarget(t, session, "resources", "resource:"+id)

	// Identical content, different mode.
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	assertMaterialChange(t, session, "resources", "resource:"+id, approved, before, workflow.CaptureOutcomeUpdate)
}

func TestCaptureFingerprintCoversGitDiffWorkingStateAtTheSameRevision(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(t *testing.T, root string)
	}{
		{"worktree patch", func(t *testing.T, root string) { writeAppFile(t, filepath.Join(root, "tracked.txt"), "edited twice\n") }},
		{"selected untracked file", func(t *testing.T, root string) { writeAppFile(t, filepath.Join(root, "notes.md"), "different notes\n") }},
	} {
		// Each case gets its own sandbox so it cannot pass on the other's
		// mutation.
		t.Run(test.name, func(t *testing.T) {
			profileDir, deps, home := resourceSandbox(t)
			root := filepath.Join(home, "dotfiles")
			ctx := context.Background()
			git := func(args ...string) {
				t.Helper()
				if _, err := deps.Runner.Run(ctx, "git", append([]string{"-C", root}, args...)...); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			git("init")
			git("config", "user.email", "test@example.invalid")
			git("config", "user.name", "Blueprint Test")
			writeAppFile(t, filepath.Join(root, "tracked.txt"), "base\n")
			git("add", "tracked.txt")
			git("commit", "-m", "initial")
			git("remote", "add", "origin", "https://github.com/example/dotfiles.git")
			writeAppFile(t, filepath.Join(root, "notes.md"), "notes\n")
			if code, out := configRun(t, deps, profileDir, "track", root, "--strategy", "git+diff", "--include-untracked", "notes.md"); code != 0 {
				t.Fatalf("track code=%d out=%s", code, out)
			}
			if code, out := configRun(t, deps, profileDir, "capture", "resources"); code != 0 {
				t.Fatalf("capture code=%d out=%s", code, out)
			}
			writeAppFile(t, filepath.Join(root, "tracked.txt"), "edited once\n")
			session := fingerprintSession(t, deps, profileDir)
			approved, before := reviewTarget(t, session, "resources", "resource:dotfiles")

			test.change(t, root)
			assertMaterialChange(t, session, "resources", "resource:dotfiles", approved, before, workflow.CaptureOutcomeUpdate)
		})
	}
}

func TestCaptureFingerprintCoversADeletedDefaultsRecordedBaseline(t *testing.T) {
	profileDir, deps := configSandbox(t)
	baselineRoot, userRoot, err := deps.ConfigDirs()
	if err != nil {
		t.Fatal(err)
	}
	// hypr/bindings.lua is an Omarchy default the user deleted on purpose,
	// so Capture would remember it as absent.
	if err := os.Remove(filepath.Join(userRoot, "hypr", "bindings.lua")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "include", "config:.config/hypr/bindings.lua"); code != 0 {
		t.Fatalf("include code=%d out=%s", code, out)
	}
	session := fingerprintSession(t, deps, profileDir)
	approved, before := reviewTarget(t, session, "config", ".config/hypr/bindings.lua")

	// Omarchy updates the default; the tombstone Capture writes records it.
	writeAppFile(t, filepath.Join(baselineRoot, "hypr", "bindings.lua"), "default, updated")
	assertMaterialChange(t, session, "config", ".config/hypr/bindings.lua", approved, before, workflow.CaptureOutcomeAbsent)
}

func TestCaptureFingerprintCoversTheShellBaseline(t *testing.T) {
	profileDir, deps := configSandbox(t)
	baseline, user, err := deps.ShellPaths()
	if err != nil {
		t.Fatal(err)
	}
	writeAppFile(t, user, strings.Replace(defaultShellJSON, `"screensaver": 150`, `"screensaver": 600`, 1))
	session := fingerprintSession(t, deps, profileDir)
	approved, before := reviewTarget(t, session, "shell", "state")

	// Omarchy changes its default; the user's document is untouched.
	writeAppFile(t, baseline, strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 900`, 1))
	assertMaterialChange(t, session, "shell", "state", approved, before, workflow.CaptureOutcomeAdd)
}

// inboundLinkSandbox tracks a copy resource with a captured inbound
// symlink ~/.current → dotfiles/a.txt.
func inboundLinkSandbox(t *testing.T) (profileDir string, deps Dependencies, retarget func(string), savedTarget func() string) {
	t.Helper()
	profileDir, deps, home := resourceSandbox(t)
	deps.ResourceLinkRoots = func(home string) []resourcesprovider.LinkSearchRoot {
		return []resourcesprovider.LinkSearchRoot{{Path: home}}
	}
	root := filepath.Join(home, "dotfiles")
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeAppFile(t, filepath.Join(root, name), name+"\n")
	}
	link := filepath.Join(home, ".current")
	retarget = func(name string) {
		t.Helper()
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, name), link); err != nil {
			t.Fatal(err)
		}
	}
	retarget("a.txt")
	if code, out := configRun(t, deps, profileDir, "track", root); code != 0 {
		t.Fatalf("track code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "resources"); code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	savedTarget = func() string {
		t.Helper()
		saved, err := profile.Load(profileDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range saved.Resources.Links {
			if item.Origin == "inbound" {
				return item.Target
			}
		}
		t.Fatalf("no inbound link saved: %#v", saved.Resources.Links)
		return ""
	}
	if got := savedTarget(); got != "a.txt" {
		t.Fatalf("captured inbound link targets %q, want a.txt", got)
	}
	return profileDir, deps, retarget, savedTarget
}

// An inbound link retargeted after approval changes what Capture would
// persist for its resource, so the approval no longer holds.
func TestCaptureApprovalRefusesAnInboundLinkRetargetedAfterReview(t *testing.T) {
	profileDir, deps, retarget, savedTarget := inboundLinkSandbox(t)
	retarget("b.txt")
	session := fingerprintSession(t, deps, profileDir)
	approved, before := reviewTarget(t, session, "resources", "resource:dotfiles")
	if before.Outcome != workflow.CaptureOutcomeUpdate {
		t.Fatalf("review outcome = %s, want update", before.Outcome)
	}

	retarget("c.txt")
	_, err := session.CaptureApproved(context.Background(), []string{"resources"}, approved)
	var changed *workflow.CaptureReviewChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("CaptureApproved error = %v, want CaptureReviewChangedError", err)
	}
	if got := savedTarget(); got != "a.txt" {
		t.Fatalf("a refused approval saved the inbound link as %q", got)
	}

	// Approving the recalculated review captures what it showed.
	if _, err := session.CaptureApproved(context.Background(), []string{"resources"}, changed.Fresh); err != nil {
		t.Fatal(err)
	}
	if got := savedTarget(); got != "c.txt" {
		t.Fatalf("re-approved Capture saved the inbound link as %q, want c.txt", got)
	}
}

// Capture Preserve freezes a resource's desired state, including the
// inbound links into it.
func TestCapturePreserveKeepsAResourcesSavedInboundLinks(t *testing.T) {
	profileDir, deps, retarget, savedTarget := inboundLinkSandbox(t)
	session := fingerprintSession(t, deps, profileDir)
	if err := session.SetPolicy(workflow.PolicyScope{}, policy.AxisCapture, "resources", "resource:dotfiles", policy.SettingDisabled); err != nil {
		t.Fatal(err)
	}
	retarget("b.txt")
	if _, err := session.CaptureMany(context.Background(), []string{"resources"}); err != nil {
		t.Fatal(err)
	}
	if got := savedTarget(); got != "a.txt" {
		t.Fatalf("Capture Preserve rewrote the inbound link to %q, want the saved a.txt", got)
	}
}
