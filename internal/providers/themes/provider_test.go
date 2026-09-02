package themes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graeme/omarchy-blueprint/internal/profile"
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
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "Osaka Jade\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}
	got, err := p.Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "overlay" || got.Items[0].Hash == "" {
		t.Fatalf("source = %q", got.Source)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "themes", "local", "osaka-jade", "colors.toml")); err != nil {
		t.Fatal(err)
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
