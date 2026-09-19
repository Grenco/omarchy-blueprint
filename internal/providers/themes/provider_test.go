package themes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type runnerFunc func(context.Context, string, ...string) (string, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f(ctx, name, args...)
}

func TestDetectBuiltinTheme(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "osaka-jade"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "Osaka Jade\n", nil }), BuiltinDir: builtin, UserDir: user}
	got, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := profile.Themes{Current: "osaka-jade", Source: "builtin", Items: []profile.Theme{{ID: "osaka-jade", Type: "builtin", Enabled: true}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("theme = %#v", got)
	}
}

func TestCaptureCopiesUserOverride(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	for _, root := range []string{builtin, user} {
		if err := os.Mkdir(filepath.Join(root, "osaka-jade"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(user, "osaka-jade", "colors.toml"), []byte("accent = '#fff'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	previous := filepath.Join(profileDir, "themes", "local", "osaka-jade", "colors.toml")
	if err := os.MkdirAll(filepath.Dir(previous), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previous, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "Osaka Jade\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}
	got, err := p.Capture(context.Background(), profile.Themes{}, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "overlay" || got.Items[0].Hash == "" {
		t.Fatalf("source = %q", got.Source)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "themes", "local", "osaka-jade", "colors.toml")); err != nil {
		t.Fatal(err)
	}
	if err := p.RollbackCapture(); err != nil {
		t.Fatal(err)
	}
	if restored, err := os.ReadFile(previous); err != nil || string(restored) != "old\n" {
		t.Fatalf("restored snapshot=%q err=%v", restored, err)
	}
}

func TestCapturePreservesExistingArtifactWhenDisabled(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(user, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "custom", "colors.toml"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	existing := filepath.Join(profileDir, "themes", "local", "custom", "colors.toml")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := profile.Themes{Current: "custom", Items: []profile.Theme{{ID: "custom", Type: "local", Hash: "old-hash"}}}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "custom\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}

	got, err := p.Capture(context.Background(), saved, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Hash != "old-hash" {
		t.Fatalf("items = %#v, want the preserved saved metadata (old hash), not a fresh re-hash of the changed local content", got.Items)
	}
	content, err := os.ReadFile(filepath.Join(profileDir, "themes", "local", "custom", "colors.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old\n" {
		t.Fatalf("staged artifact = %q, want the preserved prior content, not the changed local file", content)
	}
}

func TestCaptureTombstonesThemeRemovedLocally(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	saved := profile.Themes{Current: "nord", Items: []profile.Theme{{ID: "custom", Type: "local", Hash: "abc"}}}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "nord\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}

	got, err := p.Capture(context.Background(), saved, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Absent) != 1 || got.Absent[0].ID != "custom" || got.Absent[0].Hash != "abc" {
		t.Fatalf("absent = %#v, want a tombstone for custom preserving its prior hash", got.Absent)
	}
	for _, item := range got.Items {
		if item.ID == "custom" {
			t.Fatalf("items = %#v, want custom removed from present items", got.Items)
		}
	}
}

func TestCaptureNeverTombstonesABuiltinTheme(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(builtin, "catppuccin"), 0o755); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	// "nord" was previously known present; the machine has since switched to
	// the (also builtin) "catppuccin", so a naive merge would see nord as
	// present -> absent and tombstone it were built-ins not excluded.
	saved := profile.Themes{Current: "nord", Items: []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}}}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "catppuccin\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}

	got, err := p.Capture(context.Background(), saved, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Absent) != 0 {
		t.Fatalf("absent = %#v, want no tombstones for a builtin theme no longer active/detected", got.Absent)
	}
	found := false
	for _, item := range got.Items {
		if item.ID == "catppuccin" && item.Type == "builtin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("items = %#v, want the currently active builtin theme present", got.Items)
	}
}

func TestCaptureActiveThemeIndependentFromAvailabilityDecision(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(user, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	saved := profile.Themes{Current: "old-theme"}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "custom\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}

	got, err := p.Capture(context.Background(), saved, func(ref string) bool { return ref != "active" })
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != "old-theme" {
		t.Fatalf("current = %q, want preserved: active selection is independent from theme:custom availability", got.Current)
	}
}

func TestDiffPlanAndVerify(t *testing.T) {
	saved := profile.Themes{Current: "osaka-jade", Source: "builtin"}
	current := profile.Themes{Current: "nord", Source: "builtin"}
	if got := Diff(saved, current); len(got) != 1 {
		t.Fatalf("changes = %#v", got)
	}
	p := Provider{BuiltinDir: t.TempDir()}
	if err := os.Mkdir(filepath.Join(p.BuiltinDir, "osaka-jade"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan := p.Plan(saved, current, 1, "4.0", "4.0")
	if got := plan.Operations[0].Command; !reflect.DeepEqual(got, []string{"omarchy", "theme", "set", "osaka-jade"}) {
		t.Fatalf("command = %#v", got)
	}
	if Verify(saved, current).OK {
		t.Fatal("verification unexpectedly passed")
	}
}

func TestUserOverrideOfSavedBuiltinIsDriftButNotRemoved(t *testing.T) {
	saved := profile.Themes{Current: "osaka-jade", Source: "builtin"}
	current := profile.Themes{Current: "osaka-jade", Source: "overlay", Items: []profile.Theme{{ID: "osaka-jade", Type: "overlay", Hash: "different", Enabled: true}}}
	if got := Diff(saved, current); len(got) != 1 {
		t.Fatalf("changes = %#v", got)
	}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0")
	if len(plan.Operations) != 0 || len(plan.Skipped) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if Verify(saved, current).OK {
		t.Fatal("verification unexpectedly passed")
	}
}

// TestPlanAndVerifyIgnoreDesiredAbsenceTombstones is Task 23's PR 3 safety
// gate: a Capture-produced desired-absence tombstone is write-only today --
// Capture writes it, but Restore's Plan/Verify never read Absent -- so it
// cannot cause an unexpected removal, skip, or verification failure until
// PR 4 activates Restore-side policy.
func TestPlanAndVerifyIgnoreDesiredAbsenceTombstones(t *testing.T) {
	saved := profile.Themes{
		Current: "nord",
		Items:   []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}},
		Absent:  []profile.Theme{{ID: "gruvbox", Type: "local", Hash: "stale"}},
	}
	current := profile.Themes{Current: "nord", Items: []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0")
	if len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan = %#v, want no operations or skips for the tombstoned theme", plan)
	}
	if !Verify(saved, current).OK {
		t.Fatal("verify unexpectedly failed because of a tombstoned theme")
	}
}

func TestPlanRestoresMissingGitAndLocalThemes(t *testing.T) {
	p := Provider{BuiltinDir: t.TempDir(), UserDir: t.TempDir(), ProfileDir: t.TempDir()}
	saved := profile.Themes{Current: "custom", Items: []profile.Theme{
		{ID: "remote", Type: "git", URL: "https://example.test/omarchy-remote-theme.git", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{ID: "custom", Type: "local", Hash: "hash", Enabled: true},
	}}
	plan := p.Plan(saved, profile.Themes{Current: "nord", Items: []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}}}, 1, "4", "4")
	if len(plan.Operations) != 4 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	if plan.Operations[2].Copy == nil || plan.Operations[3].Action != "activate" {
		t.Fatalf("operations = %#v", plan.Operations)
	}
}

func TestDetectCleanGitThemeRecordsSanitizedProvenance(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	path := filepath.Join(user, "remote")
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := runnerFunc(func(_ context.Context, name string, args ...string) (string, error) {
		joined := name + " " + strings.Join(args, " ")
		switch {
		case joined == "omarchy theme current":
			return "Remote\n", nil
		case strings.HasSuffix(joined, "remote get-url origin"):
			return "https://secret@example.test/omarchy-remote-theme.git\n", nil
		case strings.HasSuffix(joined, "rev-parse HEAD"):
			return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", nil
		case strings.HasSuffix(joined, "status --porcelain --untracked-files=all"):
			return "", nil
		default:
			return "", fmt.Errorf("unexpected command: %s", joined)
		}
	})
	got, err := (Provider{Runner: runner, BuiltinDir: builtin, UserDir: user}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Type != "git" || got.Items[0].URL != "https://example.test/omarchy-remote-theme.git" {
		t.Fatalf("themes = %#v", got.Items)
	}
}
