package themes

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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
	if !reflect.DeepEqual(got, profile.Themes{Current: "osaka-jade", Source: "builtin"}) {
		t.Fatalf("theme = %#v", got)
	}
}

func TestDetectReportsUserOverrideButCaptureRejectsIt(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	for _, root := range []string{builtin, user} {
		if err := os.Mkdir(filepath.Join(root, "osaka-jade"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "Osaka Jade\n", nil }), BuiltinDir: builtin, UserDir: user}
	got, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "user" {
		t.Fatalf("source = %q", got.Source)
	}
	if err := ValidateCapture(got); err == nil {
		t.Fatal("expected capture validation error")
	}
}

func TestDiffPlanAndVerify(t *testing.T) {
	saved := profile.Themes{Current: "osaka-jade", Source: "builtin"}
	current := profile.Themes{Current: "nord", Source: "builtin"}
	if got := Diff(saved, current); len(got) != 1 {
		t.Fatalf("changes = %#v", got)
	}
	plan := Plan(saved, current, 1, "4.0", "4.0")
	if got := plan.Operations[0].Command; !reflect.DeepEqual(got, []string{"omarchy", "theme", "set", "osaka-jade"}) {
		t.Fatalf("command = %#v", got)
	}
	if Verify(saved, current).OK {
		t.Fatal("verification unexpectedly passed")
	}
}

func TestUserOverrideOfSavedBuiltinIsDriftButNotRemoved(t *testing.T) {
	saved := profile.Themes{Current: "osaka-jade", Source: "builtin"}
	current := profile.Themes{Current: "osaka-jade", Source: "user"}
	if got := Diff(saved, current); len(got) != 1 {
		t.Fatalf("changes = %#v", got)
	}
	plan := Plan(saved, current, 1, "4.0", "4.0")
	if len(plan.Operations) != 0 || len(plan.Skipped) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if Verify(saved, current).OK {
		t.Fatal("verification unexpectedly passed")
	}
}
