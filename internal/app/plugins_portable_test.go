package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	pluginsprovider "github.com/Grenco/omarchy-blueprint/internal/providers/plugins"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// Local plugins share the themes portable-snapshot contract
// (internal/portabletree); these tests hold Plugins to it end to end.

type pluginHarness struct {
	t                     *testing.T
	profileDir, pluginDir string
	runner                *machineRunner
	deps                  Dependencies
}

func newPluginHarness(t *testing.T) *pluginHarness {
	t.Helper()
	h := &pluginHarness{t: t, profileDir: t.TempDir(), pluginDir: t.TempDir()}
	state := t.TempDir()
	h.runner = &machineRunner{official: map[string]bool{}, aur: map[string]bool{}, plugins: map[string]bool{}, pluginDir: h.pluginDir}
	h.deps = Dependencies{Runner: h.runner, In: strings.NewReader(""), Now: time.Now,
		StateHome: func() (string, error) { return state, nil }, PluginDir: func() (string, error) { return h.pluginDir, nil }}
	if code, _, errout := h.run("init", h.profileDir); code != 0 {
		t.Fatalf("init: %s", errout)
	}
	return h
}

func (h *pluginHarness) run(args ...string) (int, string, string) {
	var out, errout bytes.Buffer
	h.deps.Out, h.deps.Err = &out, &errout
	code := Execute(context.Background(), args, h.deps)
	return code, out.String(), errout.String()
}

func (h *pluginHarness) capture() (int, string) {
	code, out, errout := h.run("--profile", h.profileDir, "capture", "plugins")
	return code, out + errout
}

func (h *pluginHarness) saved() profile.Plugins {
	h.t.Helper()
	data, err := profile.Load(h.profileDir)
	if err != nil {
		h.t.Fatal(err)
	}
	return data.Plugins
}

func findPlugin(items []profile.Plugin, id string) (profile.Plugin, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return profile.Plugin{}, false
}

func materializeHead(t *testing.T, repo string) (string, profile.Data) {
	t.Helper()
	service, err := profilegit.New(command.SystemRunner{}, repo)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := t.TempDir()
	if err := service.MaterializeCommit(context.Background(), gitIn(t, repo, "rev-parse", "HEAD"), snapshot); err != nil {
		t.Fatal(err)
	}
	data, err := profile.Load(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, data
}

// A representable plugin with unusual live permissions, an empty file, an
// executable and an empty folder captures to the canonical snapshot, and
// that snapshot survives commit, a fresh clone, Sync's historical
// validation and Restore, verifying against the same recorded fingerprint.
func TestPluginCaptureRoundTripsThroughGitCloneSyncAndRestore(t *testing.T) {
	h := newPluginHarness(t)
	live := filepath.Join(h.pluginDir, "acme.weather")
	writeThemeFile(t, filepath.Join(live, "manifest.json"), "{\"id\":\"acme.weather\"}\n", 0o600)
	writeThemeFile(t, filepath.Join(live, "qml", "Widget.qml"), "Item {}\n", 0o640)
	writeThemeFile(t, filepath.Join(live, "empty.conf"), "", 0o644)
	writeThemeFile(t, filepath.Join(live, "bin", "fetch.sh"), "#!/bin/sh\n", 0o700)
	for _, dir := range []string{filepath.Join(live, "cache", "deeper"), filepath.Join(live, "qml"), live} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	before := treeSnapshot(t, live)
	if code, out := h.capture(); code != 0 {
		t.Fatalf("capture: %s", out)
	}
	if treeSnapshot(t, live) != before {
		t.Fatal("Capture changed the live plugin")
	}
	artifact := filepath.Join(h.profileDir, "plugins", "local", "acme.weather")
	want := map[string]os.FileMode{".": 0o755, "manifest.json": 0o644, "qml": 0o755, "qml/Widget.qml": 0o644, "empty.conf": 0o644, "bin": 0o755, "bin/fetch.sh": 0o755}
	got := map[string]os.FileMode{}
	filepath.WalkDir(artifact, func(path string, entry os.DirEntry, err error) error {
		info, _ := entry.Info()
		rel, _ := filepath.Rel(artifact, path)
		got[filepath.ToSlash(rel)] = info.Mode().Perm()
		return nil
	})
	if len(got) != len(want) {
		t.Fatalf("artifact entries %v, want %v", got, want)
	}
	for rel, mode := range want {
		if got[rel] != mode {
			t.Fatalf("%s mode %o, want %o (all %v)", rel, got[rel], mode, got)
		}
	}
	recorded, ok := findPlugin(h.saved().Items, "acme.weather")
	if !ok || recorded.Source != "local" || recorded.Hash == "" {
		t.Fatalf("acme.weather not captured: %#v", h.saved())
	}

	// Commit, then validate exactly as Sync does: from an isolated snapshot.
	gitIn(t, h.profileDir, "init", "-b", "main")
	gitIn(t, h.profileDir, "add", ".")
	gitIn(t, h.profileDir, "commit", "-m", "plugins")
	snapshot, data := materializeHead(t, h.profileDir)
	if err := (pluginsprovider.Provider{}).ValidateSyncArtifacts(context.Background(), snapshot, data); err != nil {
		t.Fatalf("Sync rejected a captured plugin: %v", err)
	}

	// Restore from a fresh clone onto a machine without the plugin.
	clone := filepath.Join(t.TempDir(), "clone")
	gitIn(t, filepath.Dir(clone), "clone", "-q", h.profileDir, clone)
	if err := os.RemoveAll(live); err != nil {
		t.Fatal(err)
	}
	if code, out, errout := h.run("--profile", clone, "restore", "plugins", "--yes"); code != 0 {
		t.Fatalf("restore from clone: %s %s", out, errout)
	}
	if data, err := os.ReadFile(filepath.Join(live, "bin", "fetch.sh")); err != nil || string(data) != "#!/bin/sh\n" {
		t.Fatalf("restored script %q %v", data, err)
	}
	if info, err := os.Stat(filepath.Join(live, "empty.conf")); err != nil || info.Size() != 0 {
		t.Fatalf("empty file not restored: %v", err)
	}
	detected, err := (pluginsprovider.Provider{Runner: h.runner, UserDir: h.pluginDir}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if item, ok := findPlugin(detected.Items, "acme.weather"); !ok || item.Hash != recorded.Hash {
		t.Fatalf("restored plugin fingerprint %q, recorded %q", item.Hash, recorded.Hash)
	}
}

// Before the shared contract, a plugin with a non-0755 directory recorded a
// hash a fresh clone could never match, so Sync refused to start.
func TestPluginFingerprintIgnoresDirectoryModesGitCannotStore(t *testing.T) {
	h := newPluginHarness(t)
	writeThemeFile(t, filepath.Join(h.pluginDir, "acme.ordinary", "qml", "Widget.qml"), "Item {}\n", 0o644)
	private := filepath.Join(h.pluginDir, "acme.private", "qml")
	writeThemeFile(t, filepath.Join(private, "Widget.qml"), "Item {}\n", 0o644)
	if err := os.Chmod(private, 0o700); err != nil {
		t.Fatal(err)
	}
	detected, err := (pluginsprovider.Provider{Runner: h.runner, UserDir: h.pluginDir}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := findPlugin(detected.Items, "acme.ordinary")
	b, _ := findPlugin(detected.Items, "acme.private")
	if a.Hash == "" || a.Hash != b.Hash {
		t.Fatalf("directory mode changed the fingerprint: %q vs %q", a.Hash, b.Hash)
	}
}

// Plugins Git cannot represent are blocked per target: nothing is recorded,
// no artifact is written, the live directory is untouched, and a portable
// sibling plugin is still captured.
func TestPluginCaptureBlocksUnportablePlugins(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"empty": func(t *testing.T, dir string) {},
		"nested-empty-directories": func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "qml", "unused"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"nested-git-metadata": func(t *testing.T, dir string) {
			writeThemeFile(t, filepath.Join(dir, "manifest.json"), "{}\n", 0o644)
			writeThemeFile(t, filepath.Join(dir, "vendor", ".git", "HEAD"), "ref: refs/heads/main\n", 0o644)
		},
		"symlink": func(t *testing.T, dir string) {
			writeThemeFile(t, filepath.Join(dir, "manifest.json"), "{}\n", 0o644)
			if err := os.Symlink("manifest.json", filepath.Join(dir, "link.json")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			h := newPluginHarness(t)
			dir := filepath.Join(h.pluginDir, "acme.empty")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			setup(t, dir)
			writeThemeFile(t, filepath.Join(h.pluginDir, "acme.weather", "manifest.json"), "{}\n", 0o644)
			before := treeSnapshot(t, dir)
			code, out := h.capture()
			if code != 0 {
				t.Fatalf("capture: %s", out)
			}
			if !strings.Contains(out, "acme.empty") {
				t.Fatalf("capture output does not name the blocked plugin:\n%s", out)
			}
			saved := h.saved()
			if item, ok := findPlugin(append(saved.Items, saved.Absent...), "acme.empty"); ok {
				t.Fatalf("unportable plugin recorded: %#v", item)
			}
			if _, ok := findPlugin(saved.Items, "acme.weather"); !ok {
				t.Fatal("portable sibling plugin was not captured")
			}
			if _, err := os.Lstat(filepath.Join(h.profileDir, "plugins", "local", "acme.empty")); !os.IsNotExist(err) {
				t.Fatalf("artifact written for unportable plugin: %v", err)
			}
			if treeSnapshot(t, dir) != before {
				t.Fatal("Capture changed the live plugin")
			}
			code, preview, _ := h.run("--profile", h.profileDir, "capture", "plugins", "--dry-run")
			if code != 0 || !strings.Contains(preview, "fresh clone could not restore it") {
				t.Fatalf("preview lacks the target-specific reason:\n%s", preview)
			}

			// What was committed is exactly what Sync can load.
			gitIn(t, h.profileDir, "init", "-b", "main")
			gitIn(t, h.profileDir, "add", ".")
			gitIn(t, h.profileDir, "commit", "-m", "plugins")
			snapshot, data := materializeHead(t, h.profileDir)
			if err := (pluginsprovider.Provider{}).ValidateSyncArtifacts(context.Background(), snapshot, data); err != nil {
				t.Fatalf("Sync rejected the captured profile: %v", err)
			}
		})
	}
}

// A managed plugin that becomes unportable keeps its last good desired
// state and artifact; it is never turned into desired absence.
func TestPluginCaptureKeepsManagedPluginThatBecomesUnportable(t *testing.T) {
	h := newPluginHarness(t)
	dir := filepath.Join(h.pluginDir, "acme.weather")
	writeThemeFile(t, filepath.Join(dir, "manifest.json"), "{}\n", 0o644)
	if code, out := h.capture(); code != 0 {
		t.Fatalf("first capture: %s", out)
	}
	original, _ := findPlugin(h.saved().Items, "acme.weather")
	artifact := filepath.Join(h.profileDir, "plugins", "local", "acme.weather")
	firstArtifact := treeSnapshot(t, artifact)
	if err := os.Remove(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, dir)
	code, out := h.capture()
	if code != 0 || !strings.Contains(out, "acme.weather") {
		t.Fatalf("second capture code=%d:\n%s", code, out)
	}
	second := h.saved()
	if len(second.Absent) != 0 {
		t.Fatalf("emptied plugin became desired absence: %#v", second.Absent)
	}
	kept, _ := findPlugin(second.Items, "acme.weather")
	if kept.Hash == "" || kept.Hash != original.Hash {
		t.Fatalf("managed plugin desired state changed: %#v -> %#v", original, kept)
	}
	if treeSnapshot(t, artifact) != firstArtifact {
		t.Fatal("managed plugin's artifact changed")
	}
	if treeSnapshot(t, dir) != before {
		t.Fatal("Capture changed the live plugin")
	}
}

// Once policy keeps an unportable plugin out of Capture, plain Capture has
// nothing to warn about, and a preserved plugin keeps its artifact.
func TestPluginCaptureWarningRespectsPreservePolicy(t *testing.T) {
	h := newPluginHarness(t)
	if err := os.Mkdir(filepath.Join(h.pluginDir, "acme.empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeThemeFile(t, filepath.Join(h.pluginDir, "acme.weather", "manifest.json"), "{}\n", 0o644)
	if code, out := h.capture(); code != 0 || !strings.Contains(out, "Not captured") {
		t.Fatalf("missing warning before policy: %s", out)
	}
	if code, out, errout := h.run("--profile", h.profileDir, "policy", "set", "capture", "plugins", "preserve", "plugin:acme.empty", "--scope", "profile"); code != 0 {
		t.Fatalf("policy: %s %s", out, errout)
	}
	if code, out, errout := h.run("--profile", h.profileDir, "policy", "set", "capture", "plugins", "preserve", "plugin:acme.weather", "--scope", "profile"); code != 0 {
		t.Fatalf("policy: %s %s", out, errout)
	}
	artifact := filepath.Join(h.profileDir, "plugins", "local", "acme.weather")
	firstArtifact := treeSnapshot(t, artifact)
	if code, out := h.capture(); code != 0 || strings.Contains(out, "Not captured") {
		t.Fatalf("warned about a plugin policy already preserves: %s", out)
	}
	if treeSnapshot(t, artifact) != firstArtifact {
		t.Fatal("preserved plugin's artifact changed")
	}
}

// A profile captured before the contract can hold a plugin whose committed
// artifact Git dropped (it was empty). Sync still fails closed on it, and
// names only the target.
func TestPluginSyncValidationReportsMissingArtifact(t *testing.T) {
	var d profile.Data
	d.Manifest.Capture.Plugins = true
	d.Plugins.Items = []profile.Plugin{{ID: "acme.empty", Source: "local", Hash: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}}
	var artifact *syncengine.ArtifactError
	err := (pluginsprovider.Provider{}).ValidateSyncArtifacts(context.Background(), t.TempDir(), d)
	if !errors.As(err, &artifact) || artifact.Target != "plugin:acme.empty" || !artifact.Missing {
		t.Fatalf("missing plugin artifact accepted or misreported: %v", err)
	}
}
