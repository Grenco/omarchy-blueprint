package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	shellprovider "github.com/Grenco/omarchy-blueprint/internal/providers/shell"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

type machineRunner struct {
	official     map[string]bool
	aur          map[string]bool
	dependencies map[string]bool
	failInstall  string
	theme        string
	plugins      map[string]bool
	pluginDir    string
	failReload   bool
	defaults     map[string]string
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
	case "pacman -Qq":
		all := map[string]bool{}
		for pkg := range r.official {
			all[pkg] = true
		}
		for pkg := range r.aur {
			all[pkg] = true
		}
		for pkg := range r.dependencies {
			all[pkg] = true
		}
		return keys(all), nil
	case "omarchy theme current":
		return r.theme + "\n", nil
	case "omarchy plugin list --json":
		var catalog []map[string]any
		for id, enabled := range r.plugins {
			catalog = append(catalog, map[string]any{"id": id, "enabled": enabled, "firstParty": true, "canDisable": true})
		}
		if r.pluginDir != "" {
			entries, err := os.ReadDir(r.pluginDir)
			if err != nil && !os.IsNotExist(err) {
				return "", err
			}
			for _, entry := range entries {
				if entry.IsDir() {
					catalog = append(catalog, map[string]any{"id": entry.Name(), "enabled": false, "firstParty": false, "canDisable": true})
				}
			}
		}
		data, _ := json.Marshal(catalog)
		return string(data), nil
	case "hyprctl reload":
		if r.failReload {
			return "", fmt.Errorf("hyprctl reload failed")
		}
		return "", nil
	}
	if len(args) == 2 && name == "omarchy" && args[0] == "default" {
		if value, ok := r.defaults[args[1]]; ok {
			return value + "\n", nil
		}
		return "", nil
	}
	if len(args) == 4 && name == "omarchy" && args[0] == "default" && args[2] == "--install" {
		if r.defaults == nil {
			r.defaults = map[string]string{}
		}
		r.defaults[args[1]] = args[3]
		return "", nil
	}
	if len(args) == 3 && name == "omarchy" && args[0] == "default" {
		if r.defaults == nil {
			r.defaults = map[string]string{}
		}
		r.defaults[args[1]] = args[2]
		return "", nil
	}
	if len(args) == 3 && name == "omarchy" && args[0] == "plugin" && (args[1] == "enable" || args[1] == "disable") {
		if r.plugins == nil {
			r.plugins = map[string]bool{}
		}
		r.plugins[args[2]] = args[1] == "enable"
		return "", nil
	}
	if len(args) == 3 && name == "omarchy" && args[0] == "plugin" && args[1] == "validate" {
		return "", nil
	}
	if len(args) == 2 && name == "omarchy-shell" && args[0] == "shell" && args[1] == "rescanPlugins" {
		return "", nil
	}
	if name == "omarchy-restart-shell" && len(args) == 0 {
		return "", nil
	}
	if len(args) == 3 && name == "omarchy" && args[0] == "theme" && args[1] == "set" {
		r.theme = args[2]
		return "", nil
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

func TestStateProviderRegistryOrderIncludesConfigSlot(t *testing.T) {
	deps := Dependencies{Runner: &machineRunner{official: map[string]bool{}, aur: map[string]bool{}}}
	providers := stateProviders(deps, &options{profileDir: t.TempDir()})
	got := make([]string, 0, len(providers))
	for _, provider := range providers {
		got = append(got, provider.ID())
	}
	if want := []string{"packages", "themes", "plugins", "config", "defaults", "shell"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("provider order = %v, want %v", got, want)
	}
}

func TestAggregateCaptureKeepsLegacyJSONEnvelopeAndOmitsNoopConfig(t *testing.T) {
	profileDir, stateDir, builtin, user := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &machineRunner{official: map[string]bool{"zoxide": true}, aur: map[string]bool{}, theme: "Nord"}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Runner: runner, In: strings.NewReader(""), Out: &out, Err: &stderr, Now: time.Now,
		StateHome: func() (string, error) { return stateDir, nil },
		ThemeDirs: func() (string, string, error) { return builtin, user, nil },
		ConfigDirs: func() (string, string, error) {
			return filepath.Join(builtin, "config"), filepath.Join(user, ".config"), nil
		},
		ShellPaths: shellPathFunc(t),
	}
	if code := Execute(context.Background(), []string{"init", profileDir}, deps); code != 0 {
		t.Fatalf("init code=%d err=%s", code, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if code := Execute(context.Background(), []string{"--profile", profileDir, "--json", "capture"}, deps); code != 0 {
		t.Fatalf("capture code=%d err=%s", code, stderr.String())
	}
	var envelope struct {
		APIVersion int            `json:"api_version"`
		Command    string         `json:"command"`
		OK         bool           `json:"ok"`
		Data       map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("parse capture JSON: %v\n%s", err, out.String())
	}
	if envelope.APIVersion != 1 || envelope.Command != "capture" || !envelope.OK {
		t.Fatalf("legacy envelope = %#v", envelope)
	}
	for _, key := range []string{"changes", "packages", "themes", "plugins"} {
		if _, ok := envelope.Data[key]; !ok {
			t.Fatalf("capture data missing legacy key %q: %#v", key, envelope.Data)
		}
	}
	if _, ok := envelope.Data["config"]; ok {
		t.Fatalf("no-op config leaked into legacy JSON data: %#v", envelope.Data)
	}
}

func TestAggregateCaptureCapturesCustomizedConfig(t *testing.T) {
	profileDir, stateDir, builtin, user := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	baselineRoot := filepath.Join(builtin, "config")
	userRoot := filepath.Join(user, ".config")
	if err := os.MkdirAll(filepath.Join(baselineRoot, "hypr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(userRoot, "hypr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baselineRoot, "hypr", "bindings.lua"), []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &machineRunner{official: map[string]bool{"zoxide": true}, aur: map[string]bool{}, theme: "Nord"}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Runner: runner, In: strings.NewReader(""), Out: &out, Err: &stderr, Now: time.Now,
		StateHome:  func() (string, error) { return stateDir, nil },
		ThemeDirs:  func() (string, string, error) { return builtin, user, nil },
		ConfigDirs: func() (string, string, error) { return baselineRoot, userRoot, nil },
		ShellPaths: shellPathFunc(t),
	}
	if code := Execute(context.Background(), []string{"init", profileDir}, deps); code != 0 {
		t.Fatalf("init code=%d err=%s", code, stderr.String())
	}
	if code := Execute(context.Background(), []string{"--profile", profileDir, "capture"}, deps); code != 0 {
		t.Fatalf("capture code=%d err=%s", code, stderr.String())
	}
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Manifest.Capture.Config {
		t.Fatal("capture.config metadata not set")
	}
	if len(d.Config.Files) != 1 || d.Config.Files[0].Path != "hypr/bindings.lua" {
		t.Fatalf("config files = %#v", d.Config.Files)
	}
	b, err := os.ReadFile(filepath.Join(profileDir, "config", "files", "hypr", "bindings.lua"))
	if err != nil || string(b) != "custom" {
		t.Fatalf("captured file = %q err=%v", b, err)
	}
}

func TestExplicitThemeCaptureDoesNotDispatchPackages(t *testing.T) {
	profileDir, builtin, user := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &machineRunner{official: nil, aur: nil, theme: "Nord"}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Runner: runner, In: strings.NewReader(""), Out: &out, Err: &stderr, Now: time.Now,
		ThemeDirs: func() (string, string, error) { return builtin, user, nil },
	}
	if code := Execute(context.Background(), []string{"init", profileDir}, deps); code != 0 {
		t.Fatalf("init code=%d err=%s", code, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if code := Execute(context.Background(), []string{"--profile", profileDir, "capture", "themes"}, deps); code != 0 {
		t.Fatalf("capture themes code=%d err=%s", code, stderr.String())
	}
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Manifest.Capture.Themes || d.Manifest.Capture.Packages || d.Manifest.Capture.Plugins {
		t.Fatalf("capture metadata = %#v", d.Manifest.Capture)
	}
}

func TestAggregateCommandsHandlePackagesBeforePackageCapture(t *testing.T) {
	profileDir := t.TempDir()
	runner := &machineRunner{official: map[string]bool{"zoxide": true}, aur: map[string]bool{}}
	var out, stderr bytes.Buffer
	deps := Dependencies{Runner: runner, In: strings.NewReader(""), Out: &out, Err: &stderr, Now: time.Now}
	run := func(args ...string) (int, string, string) {
		out.Reset()
		stderr.Reset()
		code := Execute(context.Background(), args, deps)
		return code, out.String(), stderr.String()
	}
	if code, _, errout := run("init", profileDir); code != 0 {
		t.Fatalf("init code=%d err=%s", code, errout)
	}
	if code, output, _ := run("--profile", profileDir, "status"); code != 2 || !strings.Contains(output, "official package zoxide") {
		t.Fatalf("status code=%d output=%s", code, output)
	}
	if code, output, _ := run("--profile", profileDir, "diff"); code != 2 || !strings.Contains(output, "official package zoxide") {
		t.Fatalf("diff code=%d output=%s", code, output)
	}
	if code, output, errout := run("--profile", profileDir, "restore", "--dry-run"); code != 0 || !strings.Contains(output, "official:zoxide") {
		t.Fatalf("restore code=%d output=%s err=%s", code, output, errout)
	}
	if code, output, errout := run("--profile", profileDir, "check"); code != 0 || !strings.Contains(output, "package discovery available") {
		t.Fatalf("check code=%d output=%s err=%s", code, output, errout)
	}
}

func TestThemeVerticalSlice(t *testing.T) {
	profileDir, stateDir, builtin, user := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	for _, theme := range []string{"osaka-jade", "nord"} {
		if err := os.Mkdir(filepath.Join(builtin, theme), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runner := &machineRunner{official: map[string]bool{"zoxide": true}, aur: map[string]bool{}, theme: "Osaka Jade"}
	deps := Dependencies{
		Runner: runner, In: strings.NewReader(""), Now: time.Now,
		StateHome: func() (string, error) { return stateDir, nil },
		ThemeDirs: func() (string, string, error) { return builtin, user, nil },
		ConfigDirs: func() (string, string, error) {
			return filepath.Join(builtin, "config"), filepath.Join(user, ".config"), nil
		},
		ShellPaths: shellPathFunc(t),
	}
	run := func(args ...string) (int, string, string) {
		var out, stderr bytes.Buffer
		deps.Out, deps.Err = &out, &stderr
		return Execute(context.Background(), args, deps), out.String(), stderr.String()
	}
	if code, _, errout := run("init", profileDir); code != 0 {
		t.Fatalf("init code=%d err=%s", code, errout)
	}
	if code, out, errout := run("--profile", profileDir, "capture"); code != 0 || !strings.Contains(out, "Captured package, theme, plugin, configuration, defaults, and Shell state") {
		t.Fatalf("capture code=%d out=%s err=%s", code, out, errout)
	}
	runner.theme = "Nord"
	delete(runner.official, "zoxide")
	if code, out, _ := run("--profile", profileDir, "status"); code != 2 || !strings.Contains(out, "osaka-jade → nord") || !strings.Contains(out, "official package zoxide") {
		t.Fatalf("status code=%d out=%s", code, out)
	}
	if code, out, errout := run("--profile", profileDir, "restore", "--dry-run"); code != 0 || !strings.Contains(out, "activate theme:osaka-jade") || !strings.Contains(out, "official:zoxide") {
		t.Fatalf("dry-run code=%d out=%s err=%s", code, out, errout)
	}
	if code, out, errout := run("--profile", profileDir, "restore", "--yes"); code != 0 || !strings.Contains(out, "Restore verified") {
		t.Fatalf("restore code=%d out=%s err=%s", code, out, errout)
	}
	if runner.theme != "osaka-jade" {
		t.Fatalf("active theme = %q", runner.theme)
	}
	if !runner.official["zoxide"] {
		t.Fatal("package was not restored by aggregate restore")
	}
}

func TestLocalThemeCaptureAndRestore(t *testing.T) {
	profileDir, stateDir, builtin, user := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(user, "my-custom")
	if err := os.Mkdir(custom, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(custom, "colors.toml"), []byte("accent = '#123456'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &machineRunner{official: map[string]bool{}, aur: map[string]bool{}, theme: "my-custom"}
	deps := Dependencies{Runner: runner, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Now: time.Now,
		StateHome: func() (string, error) { return stateDir, nil }, ThemeDirs: func() (string, string, error) { return builtin, user, nil }}
	if code := Execute(context.Background(), []string{"init", profileDir}, deps); code != 0 {
		t.Fatalf("init code=%d", code)
	}
	if code := Execute(context.Background(), []string{"--profile", profileDir, "capture", "themes"}, deps); code != 0 {
		t.Fatalf("capture code=%d", code)
	}
	if err := os.RemoveAll(custom); err != nil {
		t.Fatal(err)
	}
	runner.theme = "nord"
	deps.Out, deps.Err = &bytes.Buffer{}, &bytes.Buffer{}
	if code := Execute(context.Background(), []string{"--profile", profileDir, "restore", "themes", "--yes"}, deps); code != 0 {
		t.Fatalf("restore code=%d out=%s err=%s", code, deps.Out, deps.Err)
	}
	if got, err := os.ReadFile(filepath.Join(custom, "colors.toml")); err != nil || string(got) != "accent = '#123456'\n" {
		t.Fatalf("restored file=%q err=%v", got, err)
	}
	if runner.theme != "my-custom" {
		t.Fatalf("active theme=%q", runner.theme)
	}
}

func TestPluginEnablementCaptureAndRestore(t *testing.T) {
	dir, state := t.TempDir(), t.TempDir()
	runner := &machineRunner{official: map[string]bool{}, aur: map[string]bool{}, plugins: map[string]bool{"omarchy.clock": true, "omarchy.media": false}}
	deps := Dependencies{Runner: runner, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Now: time.Now, StateHome: func() (string, error) { return state, nil }}
	if code := Execute(context.Background(), []string{"init", dir}, deps); code != 0 {
		t.Fatalf("init=%d", code)
	}
	if code := Execute(context.Background(), []string{"--profile", dir, "capture", "plugins"}, deps); code != 0 {
		t.Fatalf("capture=%d", code)
	}
	runner.plugins["omarchy.clock"] = false
	runner.plugins["omarchy.media"] = true
	if code := Execute(context.Background(), []string{"--profile", dir, "restore", "plugins", "--yes"}, deps); code != 0 {
		t.Fatalf("restore=%d err=%s", code, deps.Err)
	}
	if !runner.plugins["omarchy.clock"] || runner.plugins["omarchy.media"] {
		t.Fatalf("plugins=%#v", runner.plugins)
	}
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
	if code, out, errout := run("--profile", profileDir, "capture", "packages"); code != 0 {
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
	if code := Execute(context.Background(), []string{"--profile", dir, "capture", "packages"}, deps); code != 0 {
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
	if code := run("--profile", dir, "capture", "packages"); code != 0 {
		t.Fatalf("capture: %s", errout.String())
	}
	if code := run("--profile", dir, "exclude", "package:dislocker-git"); code != 0 {
		t.Fatalf("exclude: %s", errout.String())
	}
	if code := run("--profile", dir, "capture", "packages"); code != 0 {
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

func TestRestoreExplainsNonActionableAdditionalPackages(t *testing.T) {
	dir := t.TempDir()
	runner := &machineRunner{official: map[string]bool{"base": true}, aur: map[string]bool{}}
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
	if code := run("--profile", dir, "capture", "packages"); code != 0 {
		t.Fatalf("capture: %s", errout.String())
	}
	runner.official["sudo"] = true
	if code := run("--profile", dir, "restore", "--yes"); code != 0 {
		t.Fatalf("restore code=%d err=%s", code, errout.String())
	}
	if !strings.Contains(out.String(), "skip official:sudo (additional package left installed; removal disabled)") || !strings.Contains(out.String(), "All desired packages are installed. No changes applied.") {
		t.Fatalf("output = %s", out.String())
	}
	if code := run("--profile", dir, "status"); code != 2 {
		t.Fatalf("status code=%d out=%s", code, out.String())
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
	if code := Execute(context.Background(), []string{"--profile", profileDir, "capture", "packages"}, deps); code != 0 {
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

func configSandbox(t *testing.T) (profileDir string, deps Dependencies) {
	t.Helper()
	profileDir, stateDir, builtin, user := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	baselineRoot := filepath.Join(builtin, "config")
	userRoot := filepath.Join(user, ".config")
	if err := os.MkdirAll(filepath.Join(baselineRoot, "hypr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(userRoot, "hypr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baselineRoot, "hypr", "bindings.lua"), []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &machineRunner{official: map[string]bool{"zoxide": true}, aur: map[string]bool{}, theme: "Nord"}
	var out, stderr bytes.Buffer
	deps = Dependencies{
		Runner: runner, In: strings.NewReader(""), Out: &out, Err: &stderr, Now: time.Now,
		StateHome:  func() (string, error) { return stateDir, nil },
		ThemeDirs:  func() (string, string, error) { return builtin, user, nil },
		ConfigDirs: func() (string, string, error) { return baselineRoot, userRoot, nil },
		ShellPaths: shellPathFunc(t),
	}
	if code := Execute(context.Background(), []string{"init", profileDir}, deps); code != 0 {
		t.Fatalf("init code=%d err=%s", code, stderr.String())
	}
	return profileDir, deps
}

func configRun(t *testing.T, deps Dependencies, profileDir string, args ...string) (int, string) {
	t.Helper()
	if bt, ok := deps.Out.(*bytes.Buffer); ok {
		bt.Reset()
	}
	if bt, ok := deps.Err.(*bytes.Buffer); ok {
		bt.Reset()
	}
	code := Execute(context.Background(), append([]string{"--profile", profileDir}, args...), deps)
	var out string
	if bt, ok := deps.Out.(*bytes.Buffer); ok {
		out = bt.String()
	}
	if bt, ok := deps.Err.(*bytes.Buffer); ok {
		out += bt.String()
	}
	return code, out
}

func TestConfigVerticalSlice(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	code, out := configRun(t, deps, profileDir, "status", "config")
	if code != 1 {
		t.Fatalf("status before capture code=%d err=%s", code, out)
	}
	if !strings.Contains(out, "not been captured") {
		t.Fatalf("status output = %q", out)
	}
	code, out = configRun(t, deps, profileDir, "capture", "config")
	if code != 0 {
		t.Fatalf("capture code=%d err=%s", code, out)
	}
	// Nothing customized: nothing captured.
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Config.Files) != 0 {
		t.Fatalf("defaults captured: %#v", d.Config.Files)
	}
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out = configRun(t, deps, profileDir, "capture", "config")
	if code != 0 {
		t.Fatalf("capture code=%d err=%s", code, out)
	}
	if !strings.Contains(out, "config hypr/bindings.lua customized") {
		t.Fatalf("capture output = %q", out)
	}
	// Reset to baseline removes the stale snapshot.
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatal("capture reset failed")
	}
	if d, _ := profile.Load(profileDir); len(d.Config.Files) != 0 {
		t.Fatalf("stale snapshot kept: %#v", d.Config.Files)
	}
}

func TestConfigStatusDriftAndRestoreWithBackup(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatal("capture failed")
	}
	// User drift appears in status.
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("drifted"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, profileDir, "status", "config")
	if code != 2 || !strings.Contains(out, "differs") {
		t.Fatalf("status code=%d out=%q", code, out)
	}
	// Resetting the target to the Omarchy baseline makes replacement safe;
	// restore writes the captured customization with a backup.
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Restore to captured desired content with backup + journal.
	code, out = configRun(t, deps, profileDir, "restore", "config", "--yes")
	if code != 0 {
		t.Fatalf("restore code=%d err=%s", code, out)
	}
	b, err := os.ReadFile(filepath.Join(userRoot, "hypr", "bindings.lua"))
	if err != nil || string(b) != "custom" {
		t.Fatalf("restored file = %q err=%v", b, err)
	}
	if !strings.Contains(out, "Restore verified") {
		t.Fatalf("restore output = %q", out)
	}
	var journalPath string
	if idx := strings.LastIndex(out, "Journal: "); idx >= 0 {
		journalPath = strings.TrimSpace(out[idx+len("Journal: "):])
	}
	if journalPath == "" {
		t.Fatalf("restore output missing journal path = %q", out)
	}
	entries, err := os.ReadDir(strings.TrimSuffix(journalPath, ".jsonl") + ".backup")
	if err != nil || len(entries) == 0 {
		t.Fatalf("backup missing: %v entries=%d", err, len(entries))
	}
}

func TestConfigDryRunShowsSkipsAndReloadFailureBlocks(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatal("capture failed")
	}
	// User drift causes a safety skip in the dry run.
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("drifted"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, profileDir, "restore", "config", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run code=%d err=%s", code, out)
	}
	if !strings.Contains(out, "existing user configuration differs; overwrite disabled") {
		t.Fatalf("dry-run output = %q", out)
	}
	// Reload failure blocks completion and is reported.
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := deps.Runner.(*machineRunner)
	runner.failReload = true
	code, out = configRun(t, deps, profileDir, "restore", "config", "--yes")
	if code != 1 {
		t.Fatalf("reload-failure code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "failed operation(s)") {
		t.Fatalf("failure output = %q", out)
	}
}

func TestConfigIncludedInAggregateRestore(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatal("capture failed")
	}
	if err := os.Remove(filepath.Join(userRoot, "hypr", "bindings.lua")); err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, profileDir, "restore", "--yes")
	if code != 0 {
		t.Fatalf("aggregate restore code=%d err=%s", code, out)
	}
	b, err := os.ReadFile(filepath.Join(userRoot, "hypr", "bindings.lua"))
	if err != nil || string(b) != "custom" {
		t.Fatalf("restored file = %q err=%v", b, err)
	}
}

func TestAggregateCaptureKeepsConfigFlagWhenResetToBaseline(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture"); code != 0 {
		t.Fatalf("capture code=%d err=%s", code, out)
	}
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture"); code != 0 {
		t.Fatalf("reset capture code=%d err=%s", code, out)
	}
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Manifest.Capture.Config {
		t.Fatal("capture.config must remain true after reset to baseline")
	}
	if len(d.Config.Files) != 0 {
		t.Fatalf("config metadata must be cleared, got %#v", d.Config.Files)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "config", "files", "hypr", "bindings.lua")); !os.IsNotExist(err) {
		t.Fatal("stale captured snapshot must be removed")
	}
	if _, err := os.Stat(filepath.Join(profileDir, "config", "baseline", "hypr", "bindings.lua")); !os.IsNotExist(err) {
		t.Fatal("stale baseline snapshot must be removed")
	}
}

func TestAggregateCaptureMarksConfigBeforeCustomization(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if code, out := configRun(t, deps, profileDir, "capture"); code != 0 {
		t.Fatalf("capture code=%d err=%s", code, out)
	}
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Manifest.Capture.Config {
		t.Fatal("aggregate capture must mark config as captured even with no customizations")
	}
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, profileDir, "status", "config")
	if code != 2 || !strings.Contains(out, "customized") {
		t.Fatalf("later customization must surface as drift, code=%d out=%q", code, out)
	}
}

func TestCheckValidatesConfigSnapshotIntegrity(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatalf("capture code=%d err=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "check"); code != 0 {
		t.Fatalf("check with valid snapshot code=%d err=%s", code, out)
	}
	// Corrupt the captured snapshot: check must now fail.
	if err := os.WriteFile(filepath.Join(profileDir, "config", "files", "hypr", "bindings.lua"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "check"); code != 1 || !strings.Contains(out, "desired snapshot") {
		t.Fatalf("check with tampered snapshot code=%d out=%q", code, out)
	}
}

func TestConfigDryRunWarnsAboutReplacementVersusCreation(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatal("capture failed")
	}
	// Replacing an existing default target warns about backups.
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, profileDir, "restore", "config", "--dry-run")
	if code != 0 || !strings.Contains(out, "Existing Hyprland configuration files will be replaced; backups will be stored beside the restore journal.") {
		t.Fatalf("replacement dry-run code=%d out=%q", code, out)
	}
	// Creating a missing target warns about creation instead.
	if err := os.Remove(filepath.Join(userRoot, "hypr", "bindings.lua")); err != nil {
		t.Fatal(err)
	}
	code, out = configRun(t, deps, profileDir, "restore", "config", "--dry-run")
	if code != 0 || !strings.Contains(out, "Missing Hyprland configuration files will be created.") {
		t.Fatalf("creation dry-run code=%d out=%q", code, out)
	}
}

func TestDefaultsVerticalSlice(t *testing.T) {
	profileDir, stateDir, builtin, user := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &machineRunner{
		official: map[string]bool{"zoxide": true}, aur: map[string]bool{}, theme: "Nord",
		defaults: map[string]string{"terminal": "foot", "browser": "chromium"},
	}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Runner: runner, In: strings.NewReader(""), Out: &out, Err: &stderr, Now: time.Now,
		StateHome: func() (string, error) { return stateDir, nil },
		ThemeDirs: func() (string, string, error) { return builtin, user, nil },
		ConfigDirs: func() (string, string, error) {
			return filepath.Join(builtin, "config"), filepath.Join(user, ".config"), nil
		},
	}
	run := func(args ...string) (int, string) {
		out.Reset()
		stderr.Reset()
		code := Execute(context.Background(), append([]string{"--profile", profileDir}, args...), deps)
		return code, out.String() + stderr.String()
	}
	if code, out := run("init", profileDir); code != 0 {
		t.Fatalf("init code=%d err=%s", code, out)
	}
	// Capture: empty values are unmanaged; only terminal/browser recorded.
	if code, out := run("capture", "defaults"); code != 0 || !strings.Contains(out, "Captured defaults state") {
		t.Fatalf("capture code=%d out=%q", code, out)
	}
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Manifest.Capture.Defaults || d.Defaults.Terminal != "foot" || d.Defaults.Agent != "" {
		t.Fatalf("defaults = %#v capture=%#v", d.Defaults, d.Manifest.Capture)
	}
	// No drift yet.
	if code, out := run("status", "defaults"); code != 0 {
		t.Fatalf("status code=%d out=%q", code, out)
	}
	// Machine drifts: terminal changed on the machine, agent newly selected.
	runner.defaults["terminal"] = "ghostty"
	runner.defaults["agent"] = "codex"
	code, text := run("status", "defaults")
	if code != 2 || !strings.Contains(text, "terminal: foot → ghostty") || !strings.Contains(text, "agent: codex") {
		t.Fatalf("status code=%d out=%q", code, text)
	}
	// Dry-run plan: one native operation for terminal; agent is not managed.
	code, text = run("restore", "defaults", "--dry-run")
	if code != 0 || !strings.Contains(text, "set default:terminal") || strings.Contains(text, "default:agent") {
		t.Fatalf("dry-run code=%d out=%q", code, text)
	}
	// Restore applies only the managed drift; the machine's terminal returns
	// to the captured default and the unmanaged agent is untouched.
	code, text = run("restore", "defaults", "--yes")
	if code != 0 || !strings.Contains(text, "Restore verified") {
		t.Fatalf("restore code=%d out=%q", code, text)
	}
	if runner.defaults["terminal"] != "foot" {
		t.Fatalf("terminal default = %q, want restored captured value foot", runner.defaults["terminal"])
	}
	if runner.defaults["agent"] != "codex" {
		t.Fatal("unmanaged agent must not be touched by restore")
	}
	// Terminal is clean again; the unmanaged agent remains visible as
	// additive drift because restore never removes machine state.
	code, text = run("status", "defaults")
	if code != 2 || !strings.Contains(text, "agent: codex") || strings.Contains(text, "terminal") {
		t.Fatalf("status after restore code=%d out=%q", code, text)
	}
}

// defaultShellJSON must match the fixture in the shell provider tests.
const defaultShellJSON = `{
  "version": 1,
  "idle": {"screensaver": 150, "lock": 300},
  "bar": {
    "id": "omarchy.bar",
    "position": "top",
    "transparent": false,
    "centerAnchor": "omarchy.clock",
    "layout": {
      "left": [{"id":"omarchy.menu"}],
      "center": [{"id":"omarchy.clock","format":"HH:mm"}],
      "right": [{"id":"omarchy.audio"}]
    }
  },
  "plugins": []
}`

func shellPathsFixture(t *testing.T) (baseline, user string) {
	t.Helper()
	root := t.TempDir()
	baseline = filepath.Join(root, "baseline", "shell.json")
	user = filepath.Join(root, "user", "shell.json")
	if err := os.MkdirAll(filepath.Dir(baseline), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baseline, []byte(defaultShellJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return baseline, user
}

func shellPathFunc(t *testing.T) func() (string, string, error) {
	t.Helper()
	baseline, user := shellPathsFixture(t)
	return func() (string, string, error) { return baseline, user, nil }
}

func TestShellStatusRequiresCapture(t *testing.T) {
	profileDir, deps := configSandbox(t)
	baseline, user := shellPathsFixture(t)
	setShellPaths(&deps, baseline, user)
	code, out := configRun(t, deps, profileDir, "status", "shell")
	if code != 1 || !strings.Contains(out, "shell state has not been captured; run capture shell first") {
		t.Fatalf("status shell code=%d out=%q", code, out)
	}
}

func setShellPaths(deps *Dependencies, baseline, user string) {
	deps.ShellPaths = func() (string, string, error) { return baseline, user, nil }
}

func TestCaptureShellRejectsThirdPartyReferencesWithoutPluginCapture(t *testing.T) {
	profileDir, deps := configSandbox(t)
	baseline, user := shellPathsFixture(t)
	setShellPaths(&deps, baseline, user)
	// A customized shell.json referencing a plugin the profile has not captured.
	if err := os.WriteFile(user, []byte(strings.Replace(defaultShellJSON, `"plugins": []`, `"plugins": [{"id":"acme.weather"}]`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, profileDir, "capture", "shell")
	if code != 1 || !strings.Contains(out, "capture plugins first") {
		t.Fatalf("capture shell code=%d out=%q", code, out)
	}
}

func TestCapturedShellMakesPluginEnabledDriftOwnedByShell(t *testing.T) {
	profileDir, deps := configSandbox(t)
	baseline, user := shellPathsFixture(t)
	setShellPaths(&deps, baseline, user)
	runner := deps.Runner.(*machineRunner)
	runner.plugins = map[string]bool{"omarchy.clock": true}
	if code, out := configRun(t, deps, profileDir, "capture", "plugins"); code != 0 {
		t.Fatalf("capture plugins code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "shell"); code != 0 {
		t.Fatalf("capture shell code=%d out=%s", code, out)
	}
	// Flip only the mocked plugin enabled bit; shell.json is unchanged.
	runner.plugins["omarchy.clock"] = false
	code, out := configRun(t, deps, profileDir, "status", "plugins")
	if code != 0 {
		t.Fatalf("plugin enabled drift must be Shell-owned after capture shell, code=%d out=%q", code, out)
	}
}

func TestShellVerticalSliceRestoresReferencedLocalPluginFirst(t *testing.T) {
	profileDir, stateDir := t.TempDir(), t.TempDir()
	pluginDir := filepath.Join(t.TempDir(), "plugins")
	baseline, user := shellPathsFixture(t)
	pluginPath := filepath.Join(pluginDir, "acme.weather")
	if err := os.MkdirAll(pluginPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginPath, "manifest.json"), []byte(`{"name":"weather"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginPath, "Weather.qml"), []byte("Item {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	custom := strings.Replace(defaultShellJSON, `"plugins": []`, `"plugins": [{"id":"acme.weather"}]`, 1)
	if err := os.WriteFile(user, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	captured, err := os.ReadFile(user)
	if err != nil {
		t.Fatal(err)
	}
	runner := &machineRunner{official: map[string]bool{}, aur: map[string]bool{}, plugins: map[string]bool{}, pluginDir: pluginDir}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Runner: runner, In: strings.NewReader("yes\n"), Out: &out, Err: &stderr, Now: time.Now,
		StateHome:  func() (string, error) { return stateDir, nil },
		PluginDir:  func() (string, error) { return pluginDir, nil },
		ShellPaths: func() (string, string, error) { return baseline, user, nil },
	}
	run := func(args ...string) (int, string) {
		out.Reset()
		stderr.Reset()
		code := Execute(context.Background(), args, deps)
		return code, out.String() + stderr.String()
	}
	if code, text := run("init", profileDir); code != 0 {
		t.Fatalf("init code=%d out=%s", code, text)
	}
	if code, text := run("--profile", profileDir, "capture", "plugins"); code != 0 {
		t.Fatalf("capture plugins code=%d out=%s", code, text)
	}
	if code, text := run("--profile", profileDir, "capture", "shell"); code != 0 {
		t.Fatalf("capture shell code=%d out=%s", code, text)
	}
	if err := os.RemoveAll(pluginPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(user); err != nil {
		t.Fatal(err)
	}
	if code, text := run("--profile", profileDir, "status", "shell"); code != 2 {
		t.Fatalf("status shell code=%d out=%s", code, text)
	}
	code, text := run("--profile", profileDir, "--json", "restore", "--dry-run")
	if code != 0 {
		t.Fatalf("restore dry-run code=%d out=%s", code, text)
	}
	var envelope struct {
		Data struct {
			Plan model.RestorePlan `json:"plan"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("parse restore plan: %v\n%s", err, text)
	}
	indices := map[string]int{}
	var write model.Operation
	for i, operation := range envelope.Data.Plan.Operations {
		indices[operation.ID] = i
		if operation.ID == "shell.write" {
			write = operation
		}
	}
	if indices["plugins.validate.acme.weather"] >= indices["plugins.copy.acme.weather"] ||
		indices["plugins.copy.acme.weather"] >= indices["plugins.rescan.acme.weather"] ||
		indices["plugins.rescan.acme.weather"] >= indices["shell.write"] ||
		indices["shell.write"] >= indices["shell.restart"] {
		t.Fatalf("restore operation ordering = %#v", envelope.Data.Plan.Operations)
	}
	if len(write.DependsOn) != 1 || write.DependsOn[0] != "plugins.rescan.acme.weather" {
		t.Fatalf("shell write dependencies = %#v", write.DependsOn)
	}
	if code, text := run("--profile", profileDir, "restore", "--yes"); code != 0 || !strings.Contains(text, "Restore verified") {
		t.Fatalf("restore code=%d out=%s", code, text)
	}
	if code, text := run("--profile", profileDir, "status", "shell"); code != 0 {
		t.Fatalf("status shell code=%d out=%s", code, text)
	}
	restored, err := os.ReadFile(user)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, captured) {
		t.Fatalf("restored shell.json differs from capture\nwant=%q\ngot=%q", captured, restored)
	}
}

func TestRestoreShellAllowsMatchingInstalledPlugin(t *testing.T) {
	deps, opt, data, plan, providers := shellLinkFixture(t)
	if err := finalizeRestorePlan(context.Background(), deps, opt, data, providers, &plan, restorePlanOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 || plan.Operations[0].ID != "shell.write" || plan.Operations[1].ID != "shell.restart" {
		t.Fatalf("matching plugin removed Shell operations: %#v", plan.Operations)
	}
}

func TestRestoreShellBlocksDifferingInstalledPlugin(t *testing.T) {
	deps, opt, data, plan, providers := shellLinkFixture(t)
	pluginDir, err := deps.PluginDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "acme.weather", "Weather.qml"), []byte("different code"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := finalizeRestorePlan(context.Background(), deps, opt, data, providers, &plan, restorePlanOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("differing plugin must block Shell operations: %#v", plan.Operations)
	}
	if len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "differ from captured provenance") {
		t.Fatalf("skip = %#v", plan.Skipped)
	}
}

func shellLinkFixture(t *testing.T) (Dependencies, *options, profile.Data, model.RestorePlan, []stateProvider) {
	t.Helper()
	profileDir := t.TempDir()
	pluginDir := filepath.Join(t.TempDir(), "plugins")
	pluginPath := filepath.Join(pluginDir, "acme.weather")
	if err := os.MkdirAll(pluginPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginPath, "Weather.qml"), []byte("captured code"), 0o644); err != nil {
		t.Fatal(err)
	}
	baseline, user := shellPathsFixture(t)
	custom := strings.Replace(defaultShellJSON, `"plugins": []`, `"plugins": [{"id":"acme.weather"}]`, 1)
	if err := os.WriteFile(user, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(profileDir, "shell"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "shell", "shell.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	baselineBytes, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "shell", "baseline.json"), baselineBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	desired, err := shellprovider.ParseDocument([]byte(custom))
	if err != nil {
		t.Fatal(err)
	}
	capturedBaseline, err := shellprovider.ParseDocument(baselineBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(user); err != nil {
		t.Fatal(err)
	}
	runner := &machineRunner{official: map[string]bool{}, aur: map[string]bool{}, plugins: map[string]bool{}, pluginDir: pluginDir}
	deps := Dependencies{
		Runner:     runner,
		PluginDir:  func() (string, error) { return pluginDir, nil },
		ShellPaths: func() (string, string, error) { return baseline, user, nil },
	}
	opt := &options{profileDir: profileDir}
	provider, err := pluginProvider(deps, opt)
	if err != nil {
		t.Fatal(err)
	}
	plugins, err := provider.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data := profile.Data{Plugins: plugins, Shell: profile.Shell{Version: 1, Hash: desired.Hash, BaselineHash: capturedBaseline.Hash}}
	plan := model.RestorePlan{Operations: []model.Operation{
		{ID: "shell.write", Provider: "shell", Action: "write", File: &model.FileWrite{}},
		{ID: "shell.restart", Provider: "shell", Action: "restart", DependsOn: []string{"shell.write"}},
	}}
	providers := []stateProvider{shellStateProvider{deps: deps, opt: opt}}
	return deps, opt, data, plan, providers
}

func TestRenderShellProgressAndPlanWarnings(t *testing.T) {
	plan := model.RestorePlan{Operations: []model.Operation{
		{Provider: "shell", Action: "write", File: &model.FileWrite{Backup: true}},
		{Provider: "shell", Action: "restart"},
	}}
	if text := renderPlan(plan, true); !strings.Contains(text, "Existing Omarchy Shell configuration will be replaced") || !strings.Contains(text, "Omarchy Shell will be restarted") {
		t.Fatalf("plan warnings = %q", text)
	}
	var out bytes.Buffer
	renderProgress(&out, restore.Progress{Type: restore.ProgressStarted, Operation: plan.Operations[0]})
	renderProgress(&out, restore.Progress{Type: restore.ProgressCompleted, Operation: plan.Operations[1]})
	if text := out.String(); !strings.Contains(text, "Restoring Omarchy Shell configuration") || !strings.Contains(text, "Restarted Omarchy Shell") || strings.Contains(text, "0 shell packages") {
		t.Fatalf("shell progress = %q", text)
	}
}
