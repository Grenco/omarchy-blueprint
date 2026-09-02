package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

type machineRunner struct {
	official    map[string]bool
	aur         map[string]bool
	failInstall string
}

func (r *machineRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	switch key {
	case "omarchy version":
		return "4.0.0-1\n", nil
	case "omarchy version channel":
		return "stable\n", nil
	case "pacman -Qqen":
		return keys(r.official), nil
	case "pacman -Qqem":
		return keys(r.aur), nil
	}
	if len(args) >= 3 && name == "omarchy" && args[0] == "pkg" && args[1] == "add" {
		for _, pkg := range args[2:] {
			if pkg == r.failInstall {
				return "boom", fmt.Errorf("install failed")
			}
		}
		for _, pkg := range args[2:] {
			r.official[pkg] = true
		}
		return "", nil
	}
	if len(args) >= 4 && name == "omarchy" && args[0] == "pkg" && args[1] == "aur" && args[2] == "add" {
		for _, pkg := range args[3:] {
			if pkg == r.failInstall {
				return "boom", fmt.Errorf("install failed")
			}
		}
		for _, pkg := range args[3:] {
			r.aur[pkg] = true
		}
		return "", nil
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func keys(items map[string]bool) string {
	var out []string
	for item := range items {
		out = append(out, item)
	}
	sort.Strings(out)
	return strings.Join(out, "\n") + "\n"
}

func TestPackageVerticalSlice(t *testing.T) {
	profileDir, stateDir := t.TempDir(), t.TempDir()
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	runner := &machineRunner{official: map[string]bool{"base": true, "zoxide": true}, aur: map[string]bool{"tool-bin": true}}
	deps := Dependencies{Runner: runner, In: strings.NewReader(""), Now: func() time.Time { return now }, StateHome: func() (string, error) { return stateDir, nil }}

	run := func(args ...string) (int, string, string) {
		var out, stderr bytes.Buffer
		deps.Out, deps.Err = &out, &stderr
		code := Execute(context.Background(), args, deps)
		return code, out.String(), stderr.String()
	}
	if code, _, errout := run("init", profileDir, "--name", "main"); code != 0 {
		t.Fatalf("init code=%d err=%s", code, errout)
	}
	if code, out, errout := run("--profile", profileDir, "capture"); code != 0 {
		t.Fatalf("capture code=%d out=%s err=%s", code, out, errout)
	}

	runner.official = map[string]bool{"base": true}
	runner.aur = map[string]bool{}
	if code, out, _ := run("--profile", profileDir, "status"); code != 2 || !strings.Contains(out, "2 package differences") {
		t.Fatalf("status code=%d out=%s", code, out)
	}
	if code, out, errout := run("--profile", profileDir, "restore", "--dry-run"); code != 0 || !strings.Contains(out, "official:zoxide") || !strings.Contains(out, "aur:tool-bin") {
		t.Fatalf("dry run code=%d out=%s err=%s", code, out, errout)
	}
	if code, out, errout := run("--profile", profileDir, "restore", "--yes"); code != 0 || !strings.Contains(out, "Restore verified") {
		t.Fatalf("restore code=%d out=%s err=%s", code, out, errout)
	} else if !strings.Contains(out, "Installing 1 official package...") || !strings.Contains(out, "✓ Installed 1 aur package") {
		t.Fatalf("restore progress missing from output: %s", out)
	}
	if code, out, errout := run("--profile", profileDir, "status"); code != 0 || !strings.Contains(out, "No changes") {
		t.Fatalf("final status code=%d out=%s err=%s", code, out, errout)
	}

	entries, err := os.ReadDir(filepath.Join(stateDir, "omarchy-blueprint", "restores"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("journals = %d", len(entries))
	}
	b, err := os.ReadFile(filepath.Join(stateDir, "omarchy-blueprint", "restores", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "VERIFY_COMPLETED") {
		t.Fatalf("journal missing verification: %s", b)
	}
}

func TestJSONRestoreRequiresExplicitMode(t *testing.T) {
	dir := t.TempDir()
	runner := &machineRunner{official: map[string]bool{"base": true}, aur: map[string]bool{}}
	deps := Dependencies{Runner: runner, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Now: time.Now}
	if code := Execute(context.Background(), []string{"init", dir}, deps); code != 0 {
		t.Fatalf("init code %d", code)
	}
	runner.official["zoxide"] = true
	if code := Execute(context.Background(), []string{"--profile", dir, "capture"}, deps); code != 0 {
		t.Fatalf("capture code %d", code)
	}
	delete(runner.official, "zoxide")
	if code := Execute(context.Background(), []string{"--profile", dir, "--json", "restore"}, deps); code != 1 {
		t.Fatalf("restore code %d", code)
	}
}

func TestExcludePersistsAcrossCaptureAndCanBeIncluded(t *testing.T) {
	dir := t.TempDir()
	runner := &machineRunner{official: map[string]bool{"base": true}, aur: map[string]bool{"dislocker-git": true}}
	var out, errout bytes.Buffer
	deps := Dependencies{Runner: runner, In: strings.NewReader(""), Out: &out, Err: &errout, Now: time.Now}
	run := func(args ...string) int {
		out.Reset()
		errout.Reset()
		return Execute(context.Background(), args, deps)
	}
	if code := run("init", dir); code != 0 {
		t.Fatalf("init: %s", errout.String())
	}
	if code := run("--profile", dir, "capture"); code != 0 {
		t.Fatalf("capture: %s", errout.String())
	}
	if code := run("--profile", dir, "exclude", "package:dislocker-git"); code != 0 {
		t.Fatalf("exclude: %s", errout.String())
	}
	if code := run("--profile", dir, "capture"); code != 0 {
		t.Fatalf("recapture: %s", errout.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, "packages", "excluded.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "aur:dislocker-git\n" {
		t.Fatalf("excluded file = %q", b)
	}
	if code := run("--profile", dir, "status"); code != 0 {
		t.Fatalf("status code=%d out=%s err=%s", code, out.String(), errout.String())
	}
	if code := run("--profile", dir, "restore", "--dry-run"); code != 0 || !strings.Contains(out.String(), "skip aur:dislocker-git (excluded by profile)") {
		t.Fatalf("dry run code=%d out=%s err=%s", code, out.String(), errout.String())
	}
	if code := run("--profile", dir, "include", "aur:dislocker-git"); code != 0 {
		t.Fatalf("include: %s", errout.String())
	}
	b, err = os.ReadFile(filepath.Join(dir, "packages", "aur.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "dislocker-git\n" {
		t.Fatalf("aur file = %q", b)
	}
}

func TestRestoreContinuesAfterAURFailureAndSummarizesIt(t *testing.T) {
	profileDir, stateDir := t.TempDir(), t.TempDir()
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	runner := &machineRunner{official: map[string]bool{"base": true}, aur: map[string]bool{"broken": true, "later": true}}
	deps := Dependencies{Runner: runner, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Now: func() time.Time { return now }, StateHome: func() (string, error) { return stateDir, nil }}
	if code := Execute(context.Background(), []string{"init", profileDir}, deps); code != 0 {
		t.Fatalf("init code %d", code)
	}
	if code := Execute(context.Background(), []string{"--profile", profileDir, "capture"}, deps); code != 0 {
		t.Fatalf("capture code %d", code)
	}
	runner.aur = map[string]bool{}
	runner.failInstall = "broken"
	if code := Execute(context.Background(), []string{"--profile", profileDir, "restore", "--yes"}, deps); code != 1 {
		t.Fatalf("restore code %d", code)
	}
	if !runner.aur["later"] {
		t.Fatal("later AUR package was not installed after earlier failure")
	}
	if !strings.Contains(deps.Out.(*bytes.Buffer).String(), "1 successful and 1 failed") {
		t.Fatalf("missing summary: %s", deps.Out.(*bytes.Buffer))
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "omarchy-blueprint", "restores"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(stateDir, "omarchy-blueprint", "restores", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "OPERATION_FAILED") {
		t.Fatalf("failure missing from journal: %s", b)
	}
}
