package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type elevatedRestoreFixture struct {
	runner     *machineRunner
	stateHome  string
	profileDir string
	tty        bool
	out        bytes.Buffer
}

func newElevatedRestoreFixture(t *testing.T) *elevatedRestoreFixture {
	t.Helper()
	f := &elevatedRestoreFixture{runner: &machineRunner{official: map[string]bool{"git": true, "alacritty": true}, aur: map[string]bool{}},
		stateHome: t.TempDir(), profileDir: t.TempDir(), tty: true}
	if code, output := f.run("init", f.profileDir); code != 0 {
		t.Fatalf("init: %s", output)
	}
	if code, output := f.run("--profile", f.profileDir, "capture", "packages"); code != 0 {
		t.Fatalf("capture: %s", output)
	}
	delete(f.runner.official, "alacritty")
	return f
}

func (f *elevatedRestoreFixture) run(args ...string) (int, string) {
	f.out.Reset()
	deps := Dependencies{Runner: f.runner, In: strings.NewReader("yes\n"), Out: &f.out, Err: &f.out, Now: time.Now,
		StateHome: func() (string, error) { return f.stateHome, nil }, IsTTY: func() bool { return f.tty }}
	code := Execute(context.Background(), args, deps)
	return code, f.out.String()
}

func (f *elevatedRestoreFixture) journals(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.stateHome, "omarchy-blueprint", "restores"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return len(entries)
}

func TestFreshMachineRestoreShowsReadinessAndRefusesBeforeMutation(t *testing.T) {
	f := newElevatedRestoreFixture(t)
	f.runner.repos, f.runner.dbPath = []string{"core"}, t.TempDir()

	code, output := f.run("--profile", f.profileDir, "--json", "restore", "packages", "--dry-run")
	if code != 0 || !strings.Contains(output, `"requirements"`) || !strings.Contains(output, `"packages.metadata"`) ||
		!strings.Contains(output, `"interactive": true`) {
		t.Fatalf("dry-run code=%d output=%s", code, output)
	}
	code, output = f.run("--profile", f.profileDir, "restore", "packages", "--dry-run")
	if code != 0 || !strings.Contains(output, "Requires before applying") || !strings.Contains(output, "omarchy update") {
		t.Fatalf("human dry-run code=%d output=%s", code, output)
	}
	for _, args := range [][]string{{"restore", "packages"}, {"restore", "packages", "--yes"}, {"--json", "restore", "packages", "--yes"}} {
		code, output = f.run(append([]string{"--profile", f.profileDir}, args...)...)
		if code == 0 || !strings.Contains(output, "omarchy update") || strings.Contains(output, "Apply this restore?") {
			t.Fatalf("%v code=%d output=%s", args, code, output)
		}
	}
	if f.runner.official["alacritty"] || f.journals(t) != 0 {
		t.Fatalf("unmet readiness still mutated: installed=%v journals=%d", f.runner.official["alacritty"], f.journals(t))
	}
}

func TestPackageRestoreNeedsATerminalForElevationAndNeverTouchesCredentials(t *testing.T) {
	f := newElevatedRestoreFixture(t)
	f.tty = false
	code, output := f.run("--profile", f.profileDir, "restore", "packages", "--yes")
	if code == 0 || !strings.Contains(output, "interactive terminal") || f.runner.official["alacritty"] || f.journals(t) != 0 {
		t.Fatalf("headless elevated restore: code=%d output=%s journals=%d", code, output, f.journals(t))
	}

	f.tty = true
	code, output = f.run("--profile", f.profileDir, "restore", "packages")
	if code != 0 || !f.runner.official["alacritty"] {
		t.Fatalf("terminal restore code=%d output=%s", code, output)
	}
	if len(f.runner.interactiveCommands) != 1 || strings.Join(f.runner.interactiveCommands[0], " ") != "omarchy pkg add alacritty" {
		t.Fatalf("package install must run attached to the terminal exactly once: %#v", f.runner.interactiveCommands)
	}
	for _, command := range f.runner.allCommands {
		joined := strings.Join(command, " ")
		if strings.Contains(joined, "sudo") || strings.Contains(joined, "-Sy") || strings.Contains(joined, "omarchy update") {
			t.Fatalf("Blueprint handled elevation or metadata itself: %s", joined)
		}
	}
}
