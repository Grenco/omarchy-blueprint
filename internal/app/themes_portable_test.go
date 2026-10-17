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
	themesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/themes"
)

// themeGitFails models a theme directory whose .git has no usable history:
// Git commands fail, so detection falls back to a local snapshot.
type themeGitFails struct{ *machineRunner }

func (r themeGitFails) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "git" {
		return "", errors.New("not a usable repository")
	}
	return r.machineRunner.Run(ctx, name, args...)
}

type themeHarness struct {
	t                         *testing.T
	profileDir, builtin, user string
	runner                    *machineRunner
	deps                      Dependencies
}

func newThemeHarness(t *testing.T) *themeHarness {
	t.Helper()
	h := &themeHarness{t: t, profileDir: t.TempDir(), builtin: t.TempDir(), user: t.TempDir()}
	state := t.TempDir()
	if err := os.Mkdir(filepath.Join(h.builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.runner = &machineRunner{official: map[string]bool{}, aur: map[string]bool{}, theme: "nord"}
	h.deps = Dependencies{Runner: themeGitFails{h.runner}, In: strings.NewReader(""), Now: time.Now,
		StateHome: func() (string, error) { return state, nil }, ThemeDirs: func() (string, string, error) { return h.builtin, h.user, nil }}
	if code, _, errout := h.run("init", h.profileDir); code != 0 {
		t.Fatalf("init: %s", errout)
	}
	return h
}

func (h *themeHarness) run(args ...string) (int, string, string) {
	var out, errout bytes.Buffer
	h.deps.Out, h.deps.Err = &out, &errout
	code := Execute(context.Background(), args, h.deps)
	return code, out.String(), errout.String()
}

func (h *themeHarness) capture() (int, string) {
	code, out, errout := h.run("--profile", h.profileDir, "capture", "themes")
	return code, out + errout
}

func (h *themeHarness) saved() profile.Themes {
	h.t.Helper()
	data, err := profile.Load(h.profileDir)
	if err != nil {
		h.t.Fatal(err)
	}
	return data.Themes
}

func writeThemeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func treeSnapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		b.WriteString(rel + " " + info.Mode().String())
		if info.Mode().IsRegular() {
			data, _ := os.ReadFile(path)
			b.WriteString(" " + string(data))
		}
		b.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := (command.SystemRunner{}).Run(context.Background(), "git", append([]string{"-C", dir, "-c", "user.name=Test", "-c", "user.email=test@example.test"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

// A representable theme with unusual live permissions, an empty file, an
// executable and an empty folder captures to the canonical snapshot, and
// that snapshot survives commit, a fresh clone, Sync's historical
// validation and Restore, verifying against the same recorded fingerprint.
func TestThemeCaptureRoundTripsThroughGitCloneSyncAndRestore(t *testing.T) {
	h := newThemeHarness(t)
	live := filepath.Join(h.user, "garden")
	writeThemeFile(t, filepath.Join(live, "colors.toml"), "accent = '#123456'\n", 0o600)
	writeThemeFile(t, filepath.Join(live, "backgrounds", "dawn.jpg"), "image", 0o640)
	writeThemeFile(t, filepath.Join(live, "empty.conf"), "", 0o644)
	writeThemeFile(t, filepath.Join(live, "bin", "apply.sh"), "#!/bin/sh\n", 0o700)
	for _, dir := range []string{filepath.Join(live, "unused", "deeper"), filepath.Join(live, "backgrounds")} {
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
		t.Fatal("Capture changed the live theme")
	}
	artifact := filepath.Join(h.profileDir, "themes", "local", "garden")
	want := map[string]os.FileMode{".": 0o755, "colors.toml": 0o644, "backgrounds": 0o755, "backgrounds/dawn.jpg": 0o644, "empty.conf": 0o644, "bin": 0o755, "bin/apply.sh": 0o755}
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
	saved := h.saved()
	var recorded profile.Theme
	for _, item := range saved.Items {
		if item.ID == "garden" {
			recorded = item
		}
	}
	if recorded.Type != "local" || recorded.Hash == "" {
		t.Fatalf("garden not captured: %#v", saved)
	}

	// Commit, then validate exactly as Sync does: from an isolated snapshot.
	gitIn(t, h.profileDir, "init", "-b", "main")
	gitIn(t, h.profileDir, "add", ".")
	gitIn(t, h.profileDir, "commit", "-m", "themes")
	service, err := profilegit.New(command.SystemRunner{}, h.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := t.TempDir()
	if err := service.MaterializeCommit(context.Background(), gitIn(t, h.profileDir, "rev-parse", "HEAD"), snapshot); err != nil {
		t.Fatal(err)
	}
	data, err := profile.Load(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := (themesprovider.Provider{}).ValidateSyncArtifacts(context.Background(), snapshot, data); err != nil {
		t.Fatalf("Sync rejected a captured theme: %v", err)
	}

	// Restore from a fresh clone onto a machine without the theme.
	clone := filepath.Join(t.TempDir(), "clone")
	gitIn(t, filepath.Dir(clone), "clone", "-q", h.profileDir, clone)
	if err := os.RemoveAll(live); err != nil {
		t.Fatal(err)
	}
	if code, out, errout := h.run("--profile", clone, "restore", "themes", "--yes"); code != 0 {
		t.Fatalf("restore from clone: %s %s", out, errout)
	}
	if data, err := os.ReadFile(filepath.Join(live, "bin", "apply.sh")); err != nil || string(data) != "#!/bin/sh\n" {
		t.Fatalf("restored script %q %v", data, err)
	}
	if info, err := os.Stat(filepath.Join(live, "empty.conf")); err != nil || info.Size() != 0 {
		t.Fatalf("empty file not restored: %v", err)
	}
	detected, err := (themesprovider.Provider{Runner: h.deps.Runner, BuiltinDir: h.builtin, UserDir: h.user}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range detected.Items {
		if item.ID == "garden" && item.Hash != recorded.Hash {
			t.Fatalf("restored theme fingerprint %q, recorded %q", item.Hash, recorded.Hash)
		}
	}
}

// Themes Git cannot represent are blocked per target: nothing is recorded,
// no artifact is written, the live directory is untouched, and a portable
// sibling theme is still captured.
func TestThemeCaptureBlocksUnportableThemes(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"empty": func(t *testing.T, dir string) {},
		"nested-empty-directories": func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "backgrounds", "unused"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"git-metadata-only": func(t *testing.T, dir string) {
			writeThemeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n", 0o644)
		},
		"nested-git-metadata": func(t *testing.T, dir string) {
			writeThemeFile(t, filepath.Join(dir, "colors.toml"), "accent = '#fff'\n", 0o644)
			writeThemeFile(t, filepath.Join(dir, "vendor", ".git", "HEAD"), "ref: refs/heads/main\n", 0o644)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			h := newThemeHarness(t)
			dir := filepath.Join(h.user, "aether")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			setup(t, dir)
			writeThemeFile(t, filepath.Join(h.user, "garden", "colors.toml"), "accent = '#123456'\n", 0o644)
			before := treeSnapshot(t, dir)
			code, out := h.capture()
			if code != 0 {
				t.Fatalf("capture: %s", out)
			}
			if !strings.Contains(out, "aether") {
				t.Fatalf("capture output does not name the blocked theme:\n%s", out)
			}
			saved := h.saved()
			for _, item := range append(saved.Items, saved.Absent...) {
				if item.ID == "aether" {
					t.Fatalf("unportable theme recorded: %#v", item)
				}
			}
			found := false
			for _, item := range saved.Items {
				found = found || item.ID == "garden"
			}
			if !found {
				t.Fatal("portable sibling theme was not captured")
			}
			if _, err := os.Lstat(filepath.Join(h.profileDir, "themes", "local", "aether")); !os.IsNotExist(err) {
				t.Fatalf("artifact written for unportable theme: %v", err)
			}
			if treeSnapshot(t, dir) != before {
				t.Fatal("Capture changed the live theme")
			}
			code, preview, _ := h.run("--profile", h.profileDir, "capture", "themes", "--dry-run")
			if code != 0 || !strings.Contains(preview, "fresh clone could not restore it") {
				t.Fatalf("preview lacks the target-specific reason:\n%s", preview)
			}
		})
	}
}

// A managed theme that becomes unportable keeps its last good desired
// state and artifact; it is never turned into desired absence.
func TestThemeCaptureKeepsManagedThemeThatBecomesUnportable(t *testing.T) {
	h := newThemeHarness(t)
	dir := filepath.Join(h.user, "garden")
	writeThemeFile(t, filepath.Join(dir, "colors.toml"), "accent = '#123456'\n", 0o644)
	if code, out := h.capture(); code != 0 {
		t.Fatalf("first capture: %s", out)
	}
	first := h.saved()
	artifact := filepath.Join(h.profileDir, "themes", "local", "garden")
	firstArtifact := treeSnapshot(t, artifact)
	if err := os.Remove(filepath.Join(dir, "colors.toml")); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, dir)
	code, out := h.capture()
	if code != 0 || !strings.Contains(out, "garden") {
		t.Fatalf("second capture code=%d:\n%s", code, out)
	}
	second := h.saved()
	if len(second.Absent) != 0 {
		t.Fatalf("emptied theme became desired absence: %#v", second.Absent)
	}
	var kept profile.Theme
	for _, item := range second.Items {
		if item.ID == "garden" {
			kept = item
		}
	}
	var original profile.Theme
	for _, item := range first.Items {
		if item.ID == "garden" {
			original = item
		}
	}
	if kept.Hash == "" || kept.Hash != original.Hash {
		t.Fatalf("managed theme desired state changed: %#v -> %#v", original, kept)
	}
	if treeSnapshot(t, artifact) != firstArtifact {
		t.Fatal("managed theme's artifact changed")
	}
	if treeSnapshot(t, dir) != before {
		t.Fatal("Capture changed the live theme")
	}
}

// Once policy keeps an unportable theme out of Capture, plain Capture has
// nothing to warn about; the review still explains the safety block.
func TestThemeCaptureWarningRespectsPreservePolicy(t *testing.T) {
	h := newThemeHarness(t)
	if err := os.Mkdir(filepath.Join(h.user, "aether"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeThemeFile(t, filepath.Join(h.user, "garden", "colors.toml"), "accent = '#123456'\n", 0o644)
	if code, out := h.capture(); code != 0 || !strings.Contains(out, "Not captured") {
		t.Fatalf("missing warning before policy: %s", out)
	}
	if code, out, errout := h.run("--profile", h.profileDir, "policy", "set", "capture", "themes", "preserve", "theme:aether", "--scope", "profile"); code != 0 {
		t.Fatalf("policy: %s %s", out, errout)
	}
	if code, out := h.capture(); code != 0 || strings.Contains(out, "Not captured") {
		t.Fatalf("warned about a theme policy already preserves: %s", out)
	}
}
