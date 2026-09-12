package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	shellprovider "github.com/Grenco/omarchy-blueprint/internal/providers/shell"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
	"github.com/Grenco/omarchy-blueprint/internal/tui"
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
	miseCommands [][]string
}

// fakeBaselineHistory is opt-in: command tests retain nil-history behavior.
type fakeBaselineHistory bool

func (h fakeBaselineHistory) Match(string, string) (bool, error) { return bool(h), nil }

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "omarchy-blueprint-mise-*")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("MISE_GLOBAL_CONFIG_FILE", filepath.Join(dir, "config.toml")); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func (r *machineRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	switch key {
	case "omarchy version":
		return "4.0.0-1\n", nil
	case "omarchy version channel":
		return "stable\n", nil
	case "mise --version":
		return "2026.1.0\n", nil
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
		return "", fmt.Errorf("omarchy default does not support --install")
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
	if name == "mise" && len(args) >= 4 && args[0] == "-C" && args[1] == "/" && args[2] == "install" {
		r.miseCommands = append(r.miseCommands, append([]string{"mise"}, args...))
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
	if want := []string{"packages", "themes", "plugins", "resources", "config", "defaults", "shell", "hooks"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("provider order = %v, want %v", got, want)
	}
}

func TestRootMachineFlag(t *testing.T) {
	flag := newRoot(Dependencies{}).PersistentFlags().Lookup("machine")
	if flag == nil {
		t.Fatal("machine flag is not registered")
	}
	if flag.DefValue != "" {
		t.Errorf("machine default = %q, want empty", flag.DefValue)
	}
	if flag.Usage != "use machine overlay for this invocation" {
		t.Errorf("machine usage = %q", flag.Usage)
	}
}

func TestAutoConfigPolicyHumanAndJSON(t *testing.T) {
	profileDir := t.TempDir()
	data := profile.New("test", time.Now())
	data.Config.Included = []string{".config/nvim", ".config/nvim/lua"}
	data.Config.Excluded = []string{".config/nvim/cache"}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	var human bytes.Buffer
	deps := Dependencies{Out: &human, Now: func() time.Time { return time.Unix(1, 0) }}
	if code := Execute(context.Background(), []string{"--profile", profileDir, "auto", "config:~/.config/nvim"}, deps); code != 0 {
		t.Fatalf("human code=%d", code)
	}
	if !strings.Contains(human.String(), "Automatic config policy restored for .config/nvim.") {
		t.Fatalf("human=%q", human.String())
	}
	data, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data.Config.Included, []string{".config/nvim/lua"}) || !reflect.DeepEqual(data.Config.Excluded, []string{".config/nvim/cache"}) {
		t.Fatalf("policy=%#v", data.Config)
	}
	var jsonOut bytes.Buffer
	deps.Out = &jsonOut
	if code := Execute(context.Background(), []string{"--profile", profileDir, "--json", "auto", "config:.config/nvim/lua"}, deps); code != 0 {
		t.Fatalf("json code=%d", code)
	}
	var result struct {
		OK   bool `json:"ok"`
		Data struct {
			Path   string `json:"path"`
			Policy string `json:"policy"`
		} `json:"data"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Data.Path != ".config/nvim/lua" || result.Data.Policy != "auto" {
		t.Fatalf("json=%s", jsonOut.String())
	}
}

func TestBareInteractiveInvocationLaunchesTUI(t *testing.T) {
	var got tui.Options
	var gotDeps tui.Dependencies
	calls := 0
	deps := Dependencies{
		IsTTY: func() bool { return true },
		RunTUI: func(_ context.Context, opts tui.Options, tuiDeps tui.Dependencies) error {
			calls++
			got = opts
			gotDeps = tuiDeps
			return nil
		},
	}
	profileDir := t.TempDir()
	if code := Execute(context.Background(), []string{"--profile", filepath.Join(profileDir, "."), "--machine", "desktop"}, deps); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if calls != 1 {
		t.Fatalf("TUI calls = %d", calls)
	}
	if got != (tui.Options{ProfileDir: profileDir, Machine: "desktop"}) {
		t.Errorf("options = %#v", got)
	}
	if gotDeps.Workflow.Runner == nil || gotDeps.Workflow.StateHome == nil || gotDeps.Workflow.ResourceLinkRoots == nil {
		t.Errorf("workflow dependencies = %#v", gotDeps.Workflow)
	}
}

func TestBareNonTTYInvocationPrintsHelp(t *testing.T) {
	var out bytes.Buffer
	called := false
	deps := Dependencies{
		Out:   &out,
		IsTTY: func() bool { return false },
		RunTUI: func(context.Context, tui.Options, tui.Dependencies) error {
			called = true
			return nil
		},
	}
	if code := Execute(context.Background(), nil, deps); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if called {
		t.Error("TUI was launched")
	}
	if !strings.Contains(out.String(), "Capture and restore portable Omarchy state") {
		t.Errorf("help = %q", out.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("non-TTY help contains ANSI escape bytes: %q", out.String())
	}
}

func TestExplicitTUIRequiresTTY(t *testing.T) {
	var stderr bytes.Buffer
	deps := Dependencies{Err: &stderr, IsTTY: func() bool { return false }}
	if code := Execute(context.Background(), []string{"tui"}, deps); code != 1 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stderr.String(), "tui requires an interactive terminal") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestExplicitCLICommandNeverAutoLaunchesTUI(t *testing.T) {
	called := false
	deps := Dependencies{
		IsTTY: func() bool { return true },
		RunTUI: func(context.Context, tui.Options, tui.Dependencies) error {
			called = true
			return nil
		},
	}
	if code := Execute(context.Background(), []string{"status"}, deps); code != 1 {
		t.Fatalf("code = %d", code)
	}
	if called {
		t.Error("TUI was launched")
	}
}

func TestMachineCommandsManageOverlayAndBinding(t *testing.T) {
	profileDir, stateHome, home := t.TempDir(), t.TempDir(), t.TempDir()
	now := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	d := profile.New("test", now)
	d.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects", Kind: "directory", Strategy: "copy"}, {ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "copy"}}
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		In: strings.NewReader("\n"), Out: &out, Err: &stderr, Now: func() time.Time { return now.Add(time.Minute) },
		Hostname:  func() (string, error) { return "Framework", nil },
		HomeDir:   func() (string, error) { return home, nil },
		StateHome: func() (string, error) { return stateHome, nil },
	}
	run := func(args ...string) string {
		out.Reset()
		stderr.Reset()
		if code := Execute(context.Background(), append([]string{"--profile", profileDir}, args...), deps); code != 0 {
			t.Fatalf("%v code=%d stderr=%s", args, code, stderr.String())
		}
		return out.String()
	}
	run("machine", "add")
	loaded, err := profile.Load(profileDir)
	if err != nil || len(loaded.Machines.Items) != 1 || loaded.Machines.Items[0].Name != "framework" {
		t.Fatalf("machines = %#v, err=%v", loaded.Machines, err)
	}
	store := machine.BindingStore{StateHome: stateHome}
	if bound, err := store.Load(profileDir); err != nil || bound != "framework" {
		t.Fatalf("binding = %q, %v", bound, err)
	}
	if output := run("machine", "list"); !strings.Contains(output, "* framework") {
		t.Fatalf("list output = %q", output)
	}
	if output := run("machine", "current"); !strings.Contains(output, "framework (local binding)") {
		t.Fatalf("current output = %q", output)
	}
	run("machine", "add", "desktop", "--no-use")
	if bound, _ := store.Load(profileDir); bound != "framework" {
		t.Fatalf("binding after --no-use = %q", bound)
	}
	if output := run("--machine", "desktop", "machine", "current"); !strings.Contains(output, "desktop (explicit --machine)") {
		t.Fatalf("explicit current output = %q", output)
	}
	if bound, _ := store.Load(profileDir); bound != "framework" {
		t.Fatalf("binding after explicit selection = %q", bound)
	}
	run("machine", "use", "desktop")
	if bound, _ := store.Load(profileDir); bound != "desktop" {
		t.Fatalf("binding after use = %q", bound)
	}
	run("machine", "map", "projects", "~/Code")
	loaded, err = profile.Load(profileDir)
	if err != nil || loaded.Machines.Items[0].ResourcePaths[0].Path != "~/Code" {
		t.Fatalf("home mapping = %#v, err=%v", loaded.Machines, err)
	}
	run("machine", "map", "resource:projects", filepath.Join(home, "Work"))
	loaded, err = profile.Load(profileDir)
	if err != nil || loaded.Machines.Items[0].ResourcePaths[0].Path != "~/Work" {
		t.Fatalf("canonical home mapping = %#v, err=%v", loaded.Machines, err)
	}
	run("machine", "map", "projects", "/mnt/fast/projects")
	loaded, err = profile.Load(profileDir)
	if err != nil || !reflect.DeepEqual(loaded.Machines.Items[0].ResourcePaths, []profile.MachineResourcePath{{Resource: "projects", Path: "/mnt/fast/projects"}}) && !reflect.DeepEqual(loaded.Machines.Items[1].ResourcePaths, []profile.MachineResourcePath{{Resource: "projects", Path: "/mnt/fast/projects"}}) {
		t.Fatalf("mapped machines = %#v, err=%v", loaded.Machines, err)
	}
	run("machine", "unmap", "projects")
	loaded, err = profile.Load(profileDir)
	if err != nil || len(loaded.Machines.Items[0].ResourcePaths) != 0 || len(loaded.Machines.Items[1].ResourcePaths) != 0 {
		t.Fatalf("unmapped machines = %#v, err=%v", loaded.Machines, err)
	}
	run("machine", "rename", "desktop", "work-laptop")
	if bound, _ := store.Load(profileDir); bound != "work-laptop" {
		t.Fatalf("binding after rename = %q", bound)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "machines", "desktop.toml")); !os.IsNotExist(err) {
		t.Fatalf("old machine file remains: %v", err)
	}
	run("machine", "remove", "work-laptop")
	if bound, _ := store.Load(profileDir); bound != "" {
		t.Fatalf("binding after remove = %q", bound)
	}
	run("machine", "clear")
	if bound, err := store.Load(profileDir); err != nil || bound != "" {
		t.Fatalf("binding after clear = %q, %v", bound, err)
	}
	if output := run("--json", "machine", "current"); !strings.Contains(output, `"source": "default"`) || strings.Contains(output, stateHome) {
		t.Fatalf("current JSON = %s", output)
	}
}

func TestMachineAddPromptAndJSONRequirement(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	if err := profile.Save(profileDir, profile.New("test", time.Now())); err != nil {
		t.Fatal(err)
	}
	run := func(input string, args ...string) (int, string, string) {
		var out, stderr bytes.Buffer
		deps := Dependencies{In: strings.NewReader(input), Out: &out, Err: &stderr, StateHome: func() (string, error) { return stateHome, nil }, Hostname: func() (string, error) { return "Framework", nil }}
		return Execute(context.Background(), append([]string{"--profile", profileDir}, args...), deps), out.String(), stderr.String()
	}
	if code, out, stderr := run("\n", "machine", "add"); code != 0 || !strings.Contains(out, "Machine name [framework]:") || stderr != "" {
		t.Fatalf("blank add code=%d out=%q stderr=%q", code, out, stderr)
	}
	if code, _, stderr := run("work-laptop\n", "machine", "add"); code != 0 || stderr != "" {
		t.Fatalf("typed add code=%d stderr=%q", code, stderr)
	}
	if code, out, _ := run("ignored\n", "machine", "add", "explicit-name"); code != 0 || strings.Contains(out, "Machine name") {
		t.Fatalf("explicit add code=%d out=%q", code, out)
	}
	if code, out, stderr := run("", "--json", "machine", "add"); code == 0 || out != "" || !strings.Contains(stderr, "explicit name") {
		t.Fatalf("JSON add code=%d out=%q stderr=%q", code, out, stderr)
	}
	loaded, err := profile.Load(profileDir)
	if err != nil || len(loaded.Machines.Items) != 3 {
		t.Fatalf("machines=%#v err=%v", loaded.Machines, err)
	}
}

func TestMachineRenameAndRemoveFailuresDoNotMutate(t *testing.T) {
	profileDir := t.TempDir()
	d := profile.New("test", time.Now())
	d.Machines.Items = []profile.Machine{{Name: "one"}, {Name: "two"}}
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	deps := Dependencies{Err: &stderr}
	run := func(args ...string) int {
		stderr.Reset()
		return Execute(context.Background(), append([]string{"--profile", profileDir}, args...), deps)
	}
	if code := run("machine", "rename", "one", "two"); code == 0 {
		t.Fatal("rename to existing succeeded")
	}
	if code := run("machine", "remove", "missing"); code == 0 {
		t.Fatal("remove unknown succeeded")
	}
	loaded, err := profile.Load(profileDir)
	if err != nil || !reflect.DeepEqual(loaded.Machines.Items, d.Machines.Items) {
		t.Fatalf("machines=%#v err=%v", loaded.Machines, err)
	}
}

func TestMachineMapValidation(t *testing.T) {
	profileDir, stateHome, home := t.TempDir(), t.TempDir(), t.TempDir()
	d := profile.New("test", time.Now())
	d.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects", Kind: "directory", Strategy: "copy"}, {ID: "other", Path: "~/Code/other", Kind: "directory", Strategy: "copy"}}
	d.Machines.Items = []profile.Machine{{Name: "desktop"}}
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	deps := Dependencies{Out: &out, Err: &stderr, HomeDir: func() (string, error) { return home, nil }, StateHome: func() (string, error) { return stateHome, nil }}
	run := func(args ...string) (int, string) {
		out.Reset()
		stderr.Reset()
		code := Execute(context.Background(), append([]string{"--profile", profileDir}, args...), deps)
		return code, stderr.String()
	}
	if code, output := run("machine", "map", "projects", "~/Code"); code == 0 || !strings.Contains(output, "no machine selected; use `machine use <name>` or pass `--machine <name>`") {
		t.Fatalf("no-selection map code=%d output=%s", code, output)
	}
	if code, output := run("--machine", "desktop", "machine", "map", "unknown", "~/Code"); code == 0 || !strings.Contains(output, "not tracked") {
		t.Fatalf("unknown map code=%d output=%s", code, output)
	}
	if code, output := run("--machine", "desktop", "machine", "map", "projects", "~/Code"); code == 0 || !strings.Contains(output, "overlaps resource") {
		t.Fatalf("overlap map code=%d output=%s", code, output)
	}
}

func TestMachineContextIsReportedForResourceCommands(t *testing.T) {
	profileDir, stateHome, home := t.TempDir(), t.TempDir(), t.TempDir()
	mapped := filepath.Join(t.TempDir(), "projects")
	if err := os.MkdirAll(mapped, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapped, "README"), []byte("mapped\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := profile.New("test", time.Now())
	d.Manifest.Capture.Resources = true
	d.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects", Kind: "directory", Strategy: "copy"}}
	d.Machines.Items = []profile.Machine{{Name: "desktop", ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: mapped}, {Resource: "retired", Path: "~/Retired"}}}}
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	if err := (machine.BindingStore{StateHome: stateHome}).Save(profileDir, "desktop"); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Runner:    &machineRunner{official: map[string]bool{}, aur: map[string]bool{}},
		Out:       &out,
		Err:       &stderr,
		HomeDir:   func() (string, error) { return home, nil },
		StateHome: func() (string, error) { return stateHome, nil },
	}
	run := func(args ...string) (int, string) {
		out.Reset()
		stderr.Reset()
		return Execute(context.Background(), append([]string{"--profile", profileDir}, args...), deps), out.String()
	}
	for _, command := range [][]string{{"status", "resources"}, {"diff", "resources"}} {
		code, output := run(command...)
		if code != 2 || !strings.Contains(output, "Machine: desktop") {
			t.Fatalf("%v code=%d output=%s", command, code, output)
		}
	}
	if code, output := run("tracked"); code != 0 || !strings.Contains(output, "effective: "+mapped+" (desktop)") {
		t.Fatalf("tracked code=%d output=%s", code, output)
	}
	code, output := run("--json", "status", "resources")
	if code != 2 {
		t.Fatalf("json status code=%d output=%s", code, output)
	}
	var envelope struct {
		Data struct {
			Machine   machineOutput `json:"machine"`
			Resources struct {
				Items []struct {
					DefaultPath   string `json:"default_path"`
					EffectivePath string `json:"effective_path"`
					Machine       string `json:"machine"`
				} `json:"items"`
			} `json:"resources"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Machine != (machineOutput{Name: "desktop", Source: "binding"}) || len(envelope.Data.Resources.Items) != 1 || envelope.Data.Resources.Items[0].DefaultPath != "~/Projects" || envelope.Data.Resources.Items[0].EffectivePath != mapped || envelope.Data.Resources.Items[0].Machine != "desktop" {
		t.Fatalf("machine output=%s", output)
	}

	d.Manifest.Capture.Resources = false
	d.Resources.Items = nil
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	if code, output := run("check"); code != 0 || !strings.Contains(output, "dormant machine mappings: projects, retired") || !strings.Contains(output, "Machine: desktop") {
		t.Fatalf("check code=%d output=%s", code, output)
	}
}

func TestAggregateCaptureAndExplicitMachineOverrideUseMappedRoots(t *testing.T) {
	profileDir, stateHome, home := t.TempDir(), t.TempDir(), t.TempDir()
	laptopRoot, desktopRoot := filepath.Join(t.TempDir(), "laptop-projects"), filepath.Join(t.TempDir(), "desktop-projects")
	writeAppFile(t, filepath.Join(laptopRoot, "README"), "laptop\n")
	writeAppFile(t, filepath.Join(desktopRoot, "README"), "desktop\n")
	laptopScan, err := resourcesprovider.ScanCopyResource(laptopRoot)
	if err != nil {
		t.Fatal(err)
	}
	desktopScan, err := resourcesprovider.ScanCopyResource(desktopRoot)
	if err != nil {
		t.Fatal(err)
	}
	d := profile.New("test", time.Now())
	d.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects", Kind: "directory", Strategy: "copy"}}
	d.Machines.Items = []profile.Machine{
		{Name: "desktop", ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: desktopRoot}}},
		{Name: "laptop", ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: laptopRoot}}},
	}
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	store := machine.BindingStore{StateHome: stateHome}
	if err := store.Save(profileDir, "laptop"); err != nil {
		t.Fatal(err)
	}
	builtinThemes := t.TempDir()
	if err := os.Mkdir(filepath.Join(builtinThemes, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &machineRunner{official: map[string]bool{}, aur: map[string]bool{}, theme: "Nord"}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Runner: runner, In: strings.NewReader(""), Out: &out, Err: &stderr, Now: time.Now,
		HomeDir: func() (string, error) { return home, nil }, StateHome: func() (string, error) { return stateHome, nil },
		ThemeDirs:  func() (string, string, error) { return builtinThemes, t.TempDir(), nil },
		ConfigDirs: func() (string, string, error) { return t.TempDir(), filepath.Join(home, ".config"), nil },
		ShellPaths: shellPathFunc(t), HooksDir: func() (string, error) { return t.TempDir(), nil },
	}
	run := func(args ...string) int {
		out.Reset()
		stderr.Reset()
		return Execute(context.Background(), append([]string{"--profile", profileDir}, args...), deps)
	}
	if code := run("capture"); code != 0 {
		t.Fatalf("aggregate capture code=%d stderr=%s", code, stderr.String())
	}
	loaded, err := profile.Load(profileDir)
	if err != nil || loaded.Resources.Items[0].Hash != laptopScan.Hash || loaded.Resources.Items[0].Path != "~/Projects" {
		t.Fatalf("aggregate capture resources=%#v err=%v", loaded.Resources.Items, err)
	}
	if code := run("--machine", "desktop", "status", "resources"); code != 2 {
		t.Fatalf("explicit status code=%d stderr=%s", code, stderr.String())
	}
	if code := run("--machine", "desktop", "capture", "resources"); code != 0 {
		t.Fatalf("explicit capture code=%d stderr=%s", code, stderr.String())
	}
	loaded, err = profile.Load(profileDir)
	if err != nil || loaded.Resources.Items[0].Hash != desktopScan.Hash || loaded.Resources.Items[0].Path != "~/Projects" {
		t.Fatalf("explicit capture resources=%#v err=%v", loaded.Resources.Items, err)
	}
	if bound, err := store.Load(profileDir); err != nil || bound != "laptop" {
		t.Fatalf("binding after explicit override=%q err=%v", bound, err)
	}
}

func TestMachineMapAndCaptureAreIdempotent(t *testing.T) {
	profileDir, stateHome, home, mapped := t.TempDir(), t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "projects")
	writeAppFile(t, filepath.Join(mapped, "README"), "stable\n")
	d := profile.New("test", time.Now())
	d.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects", Kind: "directory", Strategy: "copy"}}
	d.Machines.Items = []profile.Machine{{Name: "desktop"}}
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	deps := Dependencies{Runner: &machineRunner{official: map[string]bool{}, aur: map[string]bool{}}, Out: &out, Err: &stderr, Now: time.Now, HomeDir: func() (string, error) { return home, nil }, StateHome: func() (string, error) { return stateHome, nil }}
	run := func(args ...string) {
		out.Reset()
		stderr.Reset()
		if code := Execute(context.Background(), append([]string{"--profile", profileDir}, args...), deps); code != 0 {
			t.Fatalf("%v code=%d stderr=%s", args, code, stderr.String())
		}
	}
	run("--machine", "desktop", "machine", "map", "projects", mapped)
	machinePath := filepath.Join(profileDir, "machines", "desktop.toml")
	firstMapping, err := os.ReadFile(machinePath)
	if err != nil {
		t.Fatal(err)
	}
	run("--machine", "desktop", "machine", "map", "projects", mapped)
	if got, err := os.ReadFile(machinePath); err != nil || !bytes.Equal(got, firstMapping) {
		t.Fatalf("mapping changed on repeat: %q err=%v", got, err)
	}
	run("--machine", "desktop", "capture", "resources")
	loaded, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	first := loaded.Resources
	run("--machine", "desktop", "capture", "resources")
	loaded, err = profile.Load(profileDir)
	if err != nil || !reflect.DeepEqual(loaded.Resources, first) || loaded.Resources.Items[0].Path != "~/Projects" {
		t.Fatalf("second capture=%#v first=%#v err=%v", loaded.Resources, first, err)
	}
}

func TestResourcesProviderUsesSelectedMachineMappingAndValidatesOwnership(t *testing.T) {
	profileDir, stateHome, home := t.TempDir(), t.TempDir(), t.TempDir()
	d := profile.New("test", time.Now())
	d.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects", Kind: "directory", Strategy: "copy"}}
	d.Machines.Items = []profile.Machine{{Name: "desktop", ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: "~/Code"}}}}
	if err := (machine.BindingStore{StateHome: stateHome}).Save(profileDir, "desktop"); err != nil {
		t.Fatal(err)
	}
	deps := Dependencies{
		HomeDir:    func() (string, error) { return home, nil },
		StateHome:  func() (string, error) { return stateHome, nil },
		ConfigDirs: func() (string, string, error) { return "", filepath.Join(home, ".config"), nil },
		ShellPaths: func() (string, string, error) { return "", "", nil },
		HooksDir:   func() (string, error) { return "", nil },
		ThemeDirs:  func() (string, string, error) { return "", "", nil },
		PluginDir:  func() (string, error) { return "", nil },
		ResourceLinkRoots: func(string) []resourcesprovider.LinkSearchRoot {
			return nil
		},
	}
	adapter := resourcesStateProvider{deps: deps, opt: &options{profileDir: profileDir}}
	p, err := adapter.provider(d)
	if err != nil {
		t.Fatal(err)
	}
	if p.ResourcePaths.Home != home || p.ResourcePaths.Overrides["projects"] != "~/Code" {
		t.Fatalf("resource paths = %#v", p.ResourcePaths)
	}
	d.Machines.Items[0].ResourcePaths[0].Path = "~/.config/hypr"
	if _, err := adapter.provider(d); err == nil || !strings.Contains(err.Error(), "owned by config") {
		t.Fatalf("ownership err=%v", err)
	}
}

func TestAggregateCaptureKeepsLegacyJSONEnvelopeAndOmitsNoopConfig(t *testing.T) {
	profileDir, stateDir, builtin, user, hooksDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
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
		HooksDir:   func() (string, error) { return hooksDir, nil },
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
	if _, ok := envelope.Data["config"]; !ok {
		t.Fatalf("capture JSON missing Config scan data: %#v", envelope.Data)
	}
}

func TestAggregateCaptureLeavesAmbiguousBaselineCustomizationUncaptured(t *testing.T) {
	profileDir, stateDir, builtin, user, hooksDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
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
		HooksDir:   func() (string, error) { return hooksDir, nil },
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
	if len(d.Config.Files) != 0 {
		t.Fatalf("config files = %#v", d.Config.Files)
	}
}

func TestRenderConfigProvenanceReviewIsNonDriftGuidance(t *testing.T) {
	got := renderConfigProvenanceReview(configprovider.ScanSummary{Candidates: []configprovider.Candidate{
		{Path: ".zshrc", Classification: configprovider.ConfigAmbiguousBaseline},
		{Path: ".config/example/default.conf", Classification: configprovider.ConfigAmbiguousDeletion},
	}})
	for _, want := range []string{"Baseline provenance requires review", "differs   .zshrc", "absent    .config/example/default.conf", "Not captured automatically"} {
		if !strings.Contains(got, want) {
			t.Fatalf("review output missing %q:\n%s", want, got)
		}
	}
}

func TestConfigStateProviderCapturesKnownAuthoredBaseline(t *testing.T) {
	profileDir, deps := configSandbox(t)
	baseline, user, err := deps.ConfigDirs()
	if err != nil {
		t.Fatal(err)
	}
	deps.HomeDir = func() (string, error) { return filepath.Dir(user), nil }
	deps.PluginDir = func() (string, error) { return "", fmt.Errorf("not configured") }
	if err := os.WriteFile(filepath.Join(user, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := (configStateProvider{deps: deps, opt: &options{profileDir: profileDir}}).provider(profile.Data{})
	if err != nil {
		t.Fatal(err)
	}
	p.History = fakeBaselineHistory(false)
	result, err := p.Capture(profile.Configs{})
	if err != nil || len(result.State.Files) != 1 {
		t.Fatalf("capture=%#v err=%v", result, err)
	}
	if err := os.WriteFile(filepath.Join(user, "hypr", "bindings.lua"), []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}
	scan, err := p.Scan(result.State)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.PlanOverlay(result.State, scan, 9, "4.0.0", "4.0.0", configprovider.PlanOptions{})
	if err != nil || len(plan.Operations) == 0 {
		t.Fatalf("restore plan=%#v err=%v baseline=%s", plan, err, baseline)
	}
}

func TestConfigCaptureDelegatesSavedResourceOwnership(t *testing.T) {
	profileDir, home, baseline := t.TempDir(), t.TempDir(), t.TempDir()
	userRoot := filepath.Join(home, ".config")
	for _, dir := range []string{"omarchy/themes", "omarchy/plugins", "omarchy/hooks", "nvim", "wezterm", "alacritty"} {
		if err := os.MkdirAll(filepath.Join(userRoot, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(userRoot, "nvim", "init.lua"), []byte("resource"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userRoot, "wezterm", "wezterm.lua"), []byte("config"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "dotfiles", "alacritty.toml"), filepath.Join(userRoot, "alacritty", "alacritty.toml")); err != nil {
		t.Fatal(err)
	}
	deps := Dependencies{
		ConfigDirs: func() (string, string, error) { return baseline, userRoot, nil },
		HomeDir:    func() (string, error) { return home, nil },
		ThemeDirs:  func() (string, string, error) { return "", filepath.Join(userRoot, "omarchy", "themes"), nil },
		PluginDir:  func() (string, error) { return filepath.Join(userRoot, "omarchy", "plugins"), nil },
		HooksDir:   func() (string, error) { return filepath.Join(userRoot, "omarchy", "hooks"), nil },
		ShellPaths: func() (string, string, error) { return "", filepath.Join(userRoot, "omarchy", "shell.json"), nil },
	}
	d := profile.Data{
		Config: profile.Configs{Files: []profile.ConfigFile{{Path: ".config/nvim/init.lua"}}},
		Resources: profile.Resources{
			Items: []profile.Resource{{ID: "dotfiles", Path: "~/.config/nvim", Kind: "directory", Strategy: "copy"}},
			Links: []profile.ResourceLink{{Source: "~/.config/alacritty/alacritty.toml", Origin: "inbound", TargetResource: "dotfiles"}},
		},
	}
	provider := configStateProvider{deps: deps, opt: &options{profileDir: profileDir}}
	configProvider, err := provider.provider(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(userRoot, "nvim", "init.lua"), filepath.Join(userRoot, "alacritty", "alacritty.toml")} {
		claims := configProvider.Ownership.TrackConflict(path)
		if len(claims) != 1 || claims[0].Provider != "resources" {
			t.Fatalf("resource ownership for %s = %#v", path, claims)
		}
	}
	state, _, err := provider.Capture(context.Background(), &d)
	if err != nil {
		t.Fatal(err)
	}
	result := state.(configprovider.CaptureResult)
	if len(result.State.Files) != 1 || result.State.Files[0].Path != ".config/wezterm/wezterm.lua" {
		t.Fatalf("captured config = %#v", result.State.Files)
	}
	if len(d.Resources.Items) != 1 || d.Resources.Items[0].ID != "dotfiles" {
		t.Fatalf("resources lost ownership: %#v", d.Resources)
	}
}

func TestResourceInboundHookLinkHandoff(t *testing.T) {
	profileDir, home, stateDir := t.TempDir(), t.TempDir(), t.TempDir()
	hooksDir := filepath.Join(home, ".config", "omarchy", "hooks")
	dotfiles := filepath.Join(home, "dotfiles")
	writeAppFile(t, filepath.Join(dotfiles, "hooks", "post-boot"), "resource hook")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dotfiles, "hooks", "post-boot"), filepath.Join(hooksDir, "post-boot")); err != nil {
		t.Fatal(err)
	}
	runner := &machineRunner{official: map[string]bool{}, aur: map[string]bool{}}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Runner: runner, In: strings.NewReader(""), Out: &out, Err: &stderr, Now: time.Now,
		HomeDir:   func() (string, error) { return home, nil },
		StateHome: func() (string, error) { return stateDir, nil },
		HooksDir:  func() (string, error) { return hooksDir, nil },
	}
	run := func(args ...string) (int, string) {
		out.Reset()
		stderr.Reset()
		code := Execute(context.Background(), args, deps)
		return code, out.String() + stderr.String()
	}
	if code, output := run("init", profileDir); code != 0 {
		t.Fatalf("init code=%d output=%s", code, output)
	}
	if code, output := run("--profile", profileDir, "track", dotfiles); code != 0 {
		t.Fatalf("track code=%d output=%s", code, output)
	}
	if code, output := run("--profile", profileDir, "capture", "hooks"); code != 0 || strings.Contains(output, "left unmanaged") {
		t.Fatalf("hook capture code=%d output=%s", code, output)
	}
	d, err := profile.Load(profileDir)
	if err != nil || len(d.Resources.Links) != 1 || len(d.Hooks.Items) != 0 {
		t.Fatalf("handoff profile resources=%#v hooks=%#v err=%v", d.Resources, d.Hooks, err)
	}
	if err := os.RemoveAll(dotfiles); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(hooksDir, "post-boot")); err != nil {
		t.Fatal(err)
	}
	if code, output := run("--profile", profileDir, "restore", "resources", "--yes"); code != 0 {
		t.Fatalf("resource restore code=%d output=%s", code, output)
	}
	if got := readAppFile(t, filepath.Join(hooksDir, "post-boot")); got != "resource hook" {
		t.Fatalf("restored resource hook=%q", got)
	}
}

func TestConfigOverlayAcceptance(t *testing.T) {
	t.Run("schema 7 loads restores and recaptures at the current schema", func(t *testing.T) {
		profileDir, deps, home, baseline := overlaySandbox(t)
		userRoot := filepath.Join(home, ".config")
		paths := []string{"hypr/hyprland.lua", "hypr/bindings.lua", "hypr/looknfeel.lua", "hypr/autostart.lua"}
		for _, path := range paths {
			writeAppFile(t, filepath.Join(baseline, path), "default "+path)
			writeAppFile(t, filepath.Join(userRoot, path), "default "+path)
		}
		writeAppFile(t, filepath.Join(userRoot, "hypr", "bindings.lua"), "captured")
		writeAppFile(t, filepath.Join(profileDir, "profile.toml"), "schema = 7\n\n[profile]\nname = 'legacy'\ncreated_at = 2026-09-09T00:00:00Z\nupdated_at = 2026-09-09T00:00:00Z\n\n[capture]\nconfig = true\n")
		writeAppFile(t, filepath.Join(profileDir, "config", "config.toml"), "[[file]]\nid = 'hypr.bindings'\npath = 'hypr/bindings.lua'\nhash = '"+appHash(t, "captured")+"'\nmode = '0644'\nbaseline_hash = '"+appHash(t, "default hypr/bindings.lua")+"'\nbaseline_mode = '0644'\n")
		writeAppFile(t, filepath.Join(profileDir, "config", "files", "hypr", "bindings.lua"), "captured")
		writeAppFile(t, filepath.Join(profileDir, "config", "baseline", "hypr", "bindings.lua"), "default hypr/bindings.lua")

		if code, out := configRun(t, deps, profileDir, "status", "config"); code != 0 {
			t.Fatalf("status legacy code=%d out=%s", code, out)
		}
		writeAppFile(t, filepath.Join(userRoot, "hypr", "bindings.lua"), "default hypr/bindings.lua")
		if code, out := configRun(t, deps, profileDir, "restore", "config", "--yes"); code != 0 {
			t.Fatalf("restore legacy code=%d out=%s", code, out)
		}
		if got := readAppFile(t, filepath.Join(userRoot, "hypr", "bindings.lua")); got != "captured" {
			t.Fatalf("legacy restore = %q", got)
		}
		if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
			t.Fatalf("recapture code=%d out=%s", code, out)
		}
		d, err := profile.Load(profileDir)
		if err != nil || d.Manifest.Schema != profile.Schema || len(d.Config.Files) != 0 {
			t.Fatalf("recaptured profile=%#v err=%v", d.Config, err)
		}
		if _, err := os.Stat(filepath.Join(profileDir, "config", "files", ".config", "hypr", "bindings.lua")); !os.IsNotExist(err) {
			t.Fatal("ambiguous baseline customization was recaptured")
		}
	})

	t.Run("ordinary config captures restores and ignores update backups", func(t *testing.T) {
		profileDir, deps, home, baseline := overlaySandbox(t)
		userRoot := filepath.Join(home, ".config")
		writeAppFile(t, filepath.Join(baseline, "hypr", "bindings.lua"), "default")
		writeAppFile(t, filepath.Join(userRoot, "hypr", "bindings.lua"), "custom")
		writeAppFile(t, filepath.Join(userRoot, "ghostty", "config"), "font-size=14")
		writeAppFile(t, filepath.Join(userRoot, "lazygit", "config.yml"), "gui:\n  theme: dark")
		writeAppFile(t, filepath.Join(userRoot, "hypr", "clean.lua"), "unchanged")
		writeAppFile(t, filepath.Join(baseline, "hypr", "clean.lua"), "unchanged")
		writeAppFile(t, filepath.Join(userRoot, "hypr", "bindings.lua.bak.20260909"), "backup")

		if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
			t.Fatalf("capture code=%d out=%s", code, out)
		}
		d, err := profile.Load(profileDir)
		if err != nil {
			t.Fatal(err)
		}
		if got := configPaths(d.Config.Files); !reflect.DeepEqual(got, []string{".config/ghostty/config", ".config/lazygit/config.yml"}) {
			t.Fatalf("captured paths=%v", got)
		}
		if _, err := os.Stat(filepath.Join(profileDir, "config", "files", ".config", "hypr", "bindings.lua.bak.20260909")); !os.IsNotExist(err) {
			t.Fatal("update backup was captured")
		}
		writeAppFile(t, filepath.Join(userRoot, "hypr", "bindings.lua"), "default")
		if err := os.Remove(filepath.Join(userRoot, "ghostty", "config")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(userRoot, "lazygit", "config.yml")); err != nil {
			t.Fatal(err)
		}
		if code, out := configRun(t, deps, profileDir, "restore", "config", "--yes"); code != 0 {
			t.Fatalf("restore code=%d out=%s", code, out)
		}
		if code, out := configRun(t, deps, profileDir, "status", "config"); code != 0 {
			t.Fatalf("status after restore code=%d out=%s", code, out)
		}
	})

	t.Run("ambiguous baseline customization is not captured or restored", func(t *testing.T) {
		profileDir, deps, home, baseline := overlaySandbox(t)
		userRoot := filepath.Join(home, ".config")
		path := filepath.Join(userRoot, "hypr", "bindings.lua")
		writeAppFile(t, filepath.Join(baseline, "hypr", "bindings.lua"), "one\ntwo\nthree\n")
		writeAppFile(t, path, "one\nsource\nthree\n")
		if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
			t.Fatalf("capture code=%d out=%s", code, out)
		}
		d, err := profile.Load(profileDir)
		if err != nil || len(d.Config.Files) != 0 {
			t.Fatalf("captured config=%#v err=%v", d.Config, err)
		}
		if code, out := configRun(t, deps, profileDir, "restore", "config", "--dry-run"); code != 0 || !strings.Contains(out, "No operations required") {
			t.Fatalf("restore code=%d out=%s", code, out)
		}
	})

	t.Run("resource handoff removes duplicate config ownership", func(t *testing.T) {
		profileDir, deps, home, _ := overlaySandbox(t)
		nvim := filepath.Join(home, ".config", "nvim")
		writeAppFile(t, filepath.Join(nvim, "init.lua"), "vim.opt.number = true")
		if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
			t.Fatalf("config capture code=%d out=%s", code, out)
		}
		if code, out := configRun(t, deps, profileDir, "track", nvim); code != 0 {
			t.Fatalf("track code=%d out=%s", code, out)
		}
		if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
			t.Fatalf("handoff capture code=%d out=%s", code, out)
		}
		d, err := profile.Load(profileDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Resources.Items) != 1 || d.Resources.Items[0].Path != "~/.config/nvim" || len(d.Config.Files) != 0 {
			t.Fatalf("handoff resources=%#v config=%#v", d.Resources, d.Config)
		}
		if code, out := configRun(t, deps, profileDir, "restore", "config", "--dry-run"); code != 0 || strings.Contains(out, "nvim") {
			t.Fatalf("config plan after handoff code=%d out=%s", code, out)
		}
		if code, out := configRun(t, deps, profileDir, "check"); code != 0 {
			t.Fatalf("check after handoff code=%d out=%s", code, out)
		}
	})
}

func overlaySandbox(t *testing.T) (profileDir string, deps Dependencies, home string, baseline string) {
	t.Helper()
	stateDir := t.TempDir()
	profileDir, home, baseline = t.TempDir(), t.TempDir(), t.TempDir()
	deps = Dependencies{
		Runner: &machineRunner{official: map[string]bool{}, aur: map[string]bool{}}, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Now: time.Now,
		StateHome:  func() (string, error) { return stateDir, nil },
		HomeDir:    func() (string, error) { return home, nil },
		ConfigDirs: func() (string, string, error) { return baseline, filepath.Join(home, ".config"), nil },
	}
	if code := Execute(context.Background(), []string{"init", profileDir}, deps); code != 0 {
		t.Fatalf("init code=%d", code)
	}
	return profileDir, deps, home, baseline
}

func writeAppFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readAppFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func appHash(t *testing.T, body string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%x", sum)
}

func configPaths(files []profile.ConfigFile) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}

func TestConfigCaptureJSONIncludesScanSummary(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, profileDir, "--json", "capture", "config")
	if code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	var envelope struct {
		Data struct {
			Config struct {
				Files []profile.ConfigFile `json:"files"`
				Scan  struct {
					Counts map[configprovider.Classification]int `json:"counts"`
				} `json:"scan"`
			} `json:"config"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Config.Files) != 0 || envelope.Data.Config.Scan.Counts[configprovider.ConfigAmbiguousBaseline] != 1 {
		t.Fatalf("config scan output = %#v", envelope.Data.Config)
	}
}

func TestConfigStatusAndDiffReportSkippedSurfaceOnceWithScanJSON(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	deps.HomeDir = func() (string, error) { return filepath.Dir(userRoot), nil }
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("default"), 0o644); err != nil {
		t.Fatal(err)
	}
	browser := filepath.Join(userRoot, "arbitrary-browser")
	for _, path := range []string{"Local State", "Default/Preferences", "Default/History", "Default/Cookies"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(browser, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(browser, path), []byte("state"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 1000; i++ {
		path := filepath.Join(browser, "zzz-descendants", fmt.Sprintf("descendant-%04d", i))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("state"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	code, out := configRun(t, deps, profileDir, "status", "config")
	if code != 0 || strings.Count(out, "arbitrary-browser") != 1 || strings.Contains(out, "descendant-0000") {
		t.Fatalf("status code=%d out=%s", code, out)
	}
	code, out = configRun(t, deps, profileDir, "diff", "config")
	if code != 0 || strings.Count(out, "arbitrary-browser") != 1 || strings.Contains(out, "descendant-0000") {
		t.Fatalf("diff code=%d out=%s", code, out)
	}
	code, out = configRun(t, deps, profileDir, "--json", "status", "config")
	if code != 0 {
		t.Fatalf("json status code=%d out=%s", code, out)
	}
	var envelope struct {
		Data struct {
			Config struct {
				Scan struct {
					Candidates []configprovider.Candidate      `json:"candidates"`
					Surfaces   []configprovider.SurfaceSummary `json:"surfaces"`
				} `json:"scan"`
			} `json:"config"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Config.Scan.Surfaces) != 2 || envelope.Data.Config.Scan.Surfaces[0].Path != ".config/arbitrary-browser" || envelope.Data.Config.Scan.Surfaces[0].Classification != configprovider.SurfaceStateHeavy {
		t.Fatalf("config scan = %#v", envelope.Data.Config.Scan)
	}
	for _, candidate := range envelope.Data.Config.Scan.Candidates {
		if strings.Contains(candidate.Path, "arbitrary-browser") {
			t.Fatalf("skipped descendant candidate = %#v", candidate)
		}
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
	profileDir, stateDir, builtin, user, hooksDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
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
		HooksDir:   func() (string, error) { return hooksDir, nil },
	}
	run := func(args ...string) (int, string, string) {
		var out, stderr bytes.Buffer
		deps.Out, deps.Err = &out, &stderr
		return Execute(context.Background(), args, deps), out.String(), stderr.String()
	}
	if code, _, errout := run("init", profileDir); code != 0 {
		t.Fatalf("init code=%d err=%s", code, errout)
	}
	if code, out, errout := run("--profile", profileDir, "capture"); code != 0 || !strings.Contains(out, "Captured package, theme, plugin, configuration, defaults, Shell, and hooks state") {
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

func TestConfigExcludeJSONAndPersistenceAcrossCapture(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if err := os.MkdirAll(filepath.Join(userRoot, "ghostty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userRoot, "ghostty", "config"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	code, out := configRun(t, deps, profileDir, "--json", "exclude", "config:~/.config/ghostty")
	if code != 0 {
		t.Fatalf("exclude code=%d out=%s", code, out)
	}
	var envelope struct {
		Data struct {
			Kind     string `json:"kind"`
			Path     string `json:"path"`
			Excluded bool   `json:"excluded"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope.Data.Kind != "config" || envelope.Data.Path != ".config/ghostty" || !envelope.Data.Excluded {
		t.Fatalf("json=%s err=%v", out, err)
	}
	for range 2 {
		if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
			t.Fatalf("capture code=%d out=%s", code, out)
		}
	}
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Config.Excluded, []string{".config/ghostty"}) || len(d.Config.Files) != 0 {
		t.Fatalf("config=%#v", d.Config)
	}
	if code, out := configRun(t, deps, profileDir, "include", "config:ghostty"); code != 0 || !strings.Contains(out, "Included config") {
		t.Fatalf("exact unexclude code=%d out=%s", code, out)
	}
	d, err = profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Config.Excluded) != 0 || !reflect.DeepEqual(d.Config.Included, []string{".config/ghostty"}) {
		t.Fatalf("include did not persist final policy: %#v", d.Config)
	}
}

func TestExecuteConfigHomeIncludeNormalizesAndReplacesExclusion(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	deps.HomeDir = func() (string, error) { return filepath.Dir(userRoot), nil }
	for _, input := range []string{"config:.zshrc", "config:~/.zshrc"} {
		if code, out := configRun(t, deps, profileDir, "include", input); code != 0 {
			t.Fatalf("include %s: %d %s", input, code, out)
		}
		d, err := profile.Load(profileDir)
		if err != nil || !reflect.DeepEqual(d.Config.Included, []string{".zshrc"}) {
			t.Fatalf("state=%#v err=%v", d.Config, err)
		}
	}
	if code, out := configRun(t, deps, profileDir, "exclude", "config:.zshrc"); code != 0 {
		t.Fatalf("exclude: %d %s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "include", "config:.zshrc"); code != 0 {
		t.Fatalf("include: %d %s", code, out)
	}
	d, err := profile.Load(profileDir)
	if err != nil || len(d.Config.Excluded) != 0 || !reflect.DeepEqual(d.Config.Included, []string{".zshrc"}) {
		t.Fatalf("final state=%#v err=%v", d.Config, err)
	}
}

func TestExecuteConfigIncludeResolvesAmbiguousBaselineAndDeletion(t *testing.T) {
	profileDir, deps := configSandbox(t)
	base, user, _ := deps.ConfigDirs()
	deps.HomeDir = func() (string, error) { return filepath.Dir(user), nil }
	path := "example/settings.conf"
	if err := os.MkdirAll(filepath.Join(base, "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(user, "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, path), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, path), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatal("ambiguous capture failed")
	}
	d, err := profile.Load(profileDir)
	if err != nil || len(d.Config.Files) != 0 {
		t.Fatalf("ambiguous saved: %#v %v", d.Config, err)
	}
	if code, out := configRun(t, deps, profileDir, "diff", "config"); code != 0 || !strings.Contains(out, "differs   .config/example/settings.conf") {
		t.Fatalf("diff %d: %s", code, out)
	}
	if code, _ := configRun(t, deps, profileDir, "include", "config:.config/example/settings.conf"); code != 0 {
		t.Fatal("include failed")
	}
	if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatalf("capture included: %s", out)
	}
	d, err = profile.Load(profileDir)
	if err != nil || len(d.Config.Files) != 1 || !reflect.DeepEqual(d.Config.Included, []string{".config/example/settings.conf"}) {
		t.Fatalf("captured=%#v err=%v", d.Config, err)
	}
	if err := os.WriteFile(filepath.Join(user, path), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "restore", "config", "--yes"); code != 0 {
		t.Fatalf("restore: %s", out)
	}
	b, err := os.ReadFile(filepath.Join(user, path))
	if err != nil || string(b) != "B" {
		t.Fatalf("restored=%q err=%v", b, err)
	}
	if err := os.Remove(filepath.Join(user, path)); err != nil {
		t.Fatal(err)
	}
	if code, _ := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatal("deletion ambiguity capture failed")
	}
	if code, _ := configRun(t, deps, profileDir, "include", "config:.config/example/settings.conf"); code != 0 {
		t.Fatal("deletion include failed")
	}
	if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatalf("tombstone capture: %s", out)
	}
	d, err = profile.Load(profileDir)
	if err != nil || len(d.Config.Deletes) != 1 {
		t.Fatalf("tombstone=%#v err=%v", d.Config, err)
	}
}

func TestConfigDelegatesDefaultsGeneratedOutputs(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, user, _ := deps.ConfigDirs()
	home := filepath.Dir(user)
	deps.HomeDir = func() (string, error) { return home, nil }
	deps.PluginDir = func() (string, error) { return "", fmt.Errorf("not configured") }
	for _, path := range []string{"xdg-terminals.list", "brave-flags.conf", "environment.d/omarchy-firefox-wayland.conf", "nvim/init.lua"} {
		full := filepath.Join(user, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("value"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := (configStateProvider{deps: deps, opt: &options{profileDir: profileDir}}).provider(profile.Data{})
	if err != nil {
		t.Fatal(err)
	}
	scan, err := p.Scan(profile.Configs{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]configprovider.Classification{}
	for _, candidate := range scan.Candidates {
		got[candidate.Path] = candidate.Classification
	}
	for _, path := range []string{".config/xdg-terminals.list", ".config/brave-flags.conf", ".config/environment.d/omarchy-firefox-wayland.conf"} {
		if _, exists := got[path]; exists {
			t.Fatalf("Defaults-generated path escaped delegation: %s=%s", path, got[path])
		}
	}
	if got[".config/nvim/init.lua"] != configprovider.ConfigAdded {
		t.Fatalf("unrelated config classification=%s", got[".config/nvim/init.lua"])
	}
}

func TestPackageExcludeHintsRelatedConfigWithoutExcludingIt(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if err := os.MkdirAll(filepath.Join(userRoot, "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userRoot, "nvim", "init.lua"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	deps.Runner.(*machineRunner).official["neovim"] = true
	if code, out := configRun(t, deps, profileDir, "capture", "packages"); code != 0 {
		t.Fatalf("capture packages code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "exclude", "official:neovim"); code != 0 || !strings.Contains(out, "Related Config state remains included:\n  ~/.config/nvim\nRun:\n  omarchy-blueprint exclude config:nvim") {
		t.Fatalf("exclude code=%d out=%s", code, out)
	}
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Config.Files) != 1 || d.Config.Files[0].Path != "nvim/init.lua" || len(d.Config.Excluded) != 0 {
		t.Fatalf("config changed by package exclusion: %#v", d.Config)
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
	profileDir, stateDir, builtin, user, hooksDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
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
		HooksDir:   func() (string, error) { return hooksDir, nil },
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
	if !strings.Contains(out, "Baseline provenance requires review") {
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
	if code != 0 || !strings.Contains(out, "Baseline provenance requires review") {
		t.Fatalf("status code=%d out=%q", code, out)
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
	if !strings.Contains(out, "No operations required") {
		t.Fatalf("dry-run output = %q", out)
	}
}

func TestConfigForceDryRunReplacesUnknownTarget(t *testing.T) {
	profileDir, deps := configSandbox(t)
	_, userRoot, _ := deps.ConfigDirs()
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("captured"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "config"); code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	if err := os.WriteFile(filepath.Join(userRoot, "hypr", "bindings.lua"), []byte("target-only"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, profileDir, "restore", "config", "--dry-run")
	if code != 0 || !strings.Contains(out, "No operations required") {
		t.Fatalf("safe dry-run code=%d out=%q", code, out)
	}
	code, out = configRun(t, deps, profileDir, "restore", "config", "--force", "--dry-run")
	if code != 0 || !strings.Contains(out, "No operations required") {
		t.Fatalf("force dry-run code=%d out=%q", code, out)
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
	if _, err := os.Stat(filepath.Join(userRoot, "hypr", "bindings.lua")); !os.IsNotExist(err) {
		t.Fatalf("ambiguous config restored: %v", err)
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
	if code != 0 || !strings.Contains(out, "Baseline provenance requires review") {
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
		t.Fatalf("check with no ambiguous snapshot code=%d out=%q", code, out)
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
	if code != 0 || !strings.Contains(out, "No operations required") {
		t.Fatalf("replacement dry-run code=%d out=%q", code, out)
	}
	// A missing target remains a no-op without a captured ambiguous file.
	if err := os.Remove(filepath.Join(userRoot, "hypr", "bindings.lua")); err != nil {
		t.Fatal(err)
	}
	code, out = configRun(t, deps, profileDir, "restore", "config", "--dry-run")
	if code != 0 || !strings.Contains(out, "No operations required") {
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
		{ID: "shell.restart", Provider: "shell", Action: "restart", Command: []string{"omarchy-restart-shell"}, DependsOn: []string{"shell.write"}},
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

func TestRenderShellConflictAndForceWarning(t *testing.T) {
	plan := model.RestorePlan{
		Operations: []model.Operation{{Provider: "shell", Action: "write"}},
		Skipped:    []model.Skipped{{Provider: "shell", Resource: "shell:idle.lock", Reason: "changed independently on this machine; keeping the current value (use --force to apply captured intent)"}},
	}
	text := renderPlanWithOptions(plan, true, restorePlanOptions{Force: true})
	if !strings.Contains(text, "idle.lock changed independently on this machine; keeping the current value") || !strings.Contains(text, "Force enabled: conflicting Shell values") || strings.Contains(text, "whole file") {
		t.Fatalf("rendered=%q", text)
	}
}

func TestLaptopToDesktopShellMergePreservesTargetLayoutAndResolvesConflict(t *testing.T) {
	profileDir, deps := configSandbox(t)
	baseline, user := shellPathsFixture(t)
	setShellPaths(&deps, baseline, user)
	source := strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 600`, 1)
	source = strings.Replace(source, `"position": "top"`, `"position": "bottom"`, 1)
	if err := os.WriteFile(user, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "shell"); code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	target := strings.Replace(defaultShellJSON, `"lock": 300`, `"lock": 900`, 1)
	target = strings.Replace(target, `{"id":"omarchy.audio"}`, `{"id":"desktop.widget"}`, 1)
	if err := os.WriteFile(user, []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, profileDir, "restore", "shell", "--yes")
	if code != 2 || !strings.Contains(out, "Safe Shell changes were restored") {
		t.Fatalf("normal restore code=%d out=%s", code, out)
	}
	merged, err := shellprovider.ReadDocument(user)
	if err != nil {
		t.Fatal(err)
	}
	idle := merged.Value["idle"].(map[string]any)
	bar := merged.Value["bar"].(map[string]any)
	if shellCanonical(idle["lock"]) != "900" || bar["position"] != "bottom" || !strings.Contains(shellCanonical(bar["layout"]), "desktop.widget") {
		t.Fatalf("normal merged state=%#v", merged.Value)
	}
	if code, out := configRun(t, deps, profileDir, "status", "shell"); code != 2 || !strings.Contains(out, "idle.lock") {
		t.Fatalf("status conflict code=%d out=%s", code, out)
	}
	code, out = configRun(t, deps, profileDir, "restore", "shell", "--force", "--yes")
	if code != 0 || !strings.Contains(out, "Restore verified") {
		t.Fatalf("forced restore code=%d out=%s", code, out)
	}
	merged, err = shellprovider.ReadDocument(user)
	if err != nil {
		t.Fatal(err)
	}
	idle = merged.Value["idle"].(map[string]any)
	bar = merged.Value["bar"].(map[string]any)
	if shellCanonical(idle["lock"]) != "600" || bar["position"] != "bottom" || !strings.Contains(shellCanonical(bar["layout"]), "desktop.widget") {
		t.Fatalf("forced merged state=%#v", merged.Value)
	}
	if code, out := configRun(t, deps, profileDir, "status", "shell"); code != 0 {
		t.Fatalf("status after force code=%d out=%s", code, out)
	}
}

func TestLaptopToDesktopShellMergeMovesWidgetWithoutForce(t *testing.T) {
	profileDir, deps := configSandbox(t)
	baseline, user := shellPathsFixture(t)
	setShellPaths(&deps, baseline, user)
	pluginDir := filepath.Join(t.TempDir(), "plugins")
	pluginPath := filepath.Join(pluginDir, "acme.weather")
	if err := os.MkdirAll(pluginPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginPath, "Weather.qml"), []byte("Item {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := deps.Runner.(*machineRunner)
	runner.pluginDir = pluginDir
	deps.PluginDir = func() (string, error) { return pluginDir, nil }
	source := strings.Replace(defaultShellJSON, `[{"id":"omarchy.clock","format":"HH:mm"}]`, `[{"id":"omarchy.clock","format":"HH:mm"},{"id":"acme.weather","units":"celsius"}]`, 1)
	if err := os.WriteFile(user, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "plugins"); code != 0 {
		t.Fatalf("capture plugins code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "shell"); code != 0 {
		t.Fatalf("capture shell code=%d out=%s", code, out)
	}
	target := strings.Replace(defaultShellJSON, `[{"id":"omarchy.audio"}]`, `[{"id":"desktop.only"},"acme.weather"]`, 1)
	if err := os.WriteFile(user, []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "restore", "shell", "--yes"); code != 0 {
		t.Fatalf("restore code=%d out=%s", code, out)
	}
	merged, err := shellprovider.ReadDocument(user)
	if err != nil {
		t.Fatal(err)
	}
	bar := merged.Value["bar"].(map[string]any)
	layout := bar["layout"].(map[string]any)
	if strings.Count(shellCanonical(layout), "acme.weather") != 1 || !strings.Contains(shellCanonical(layout["center"]), "acme.weather") || !strings.Contains(shellCanonical(layout["right"]), "desktop.only") {
		t.Fatalf("merged layout=%#v", layout)
	}
	if code, out := configRun(t, deps, profileDir, "status", "shell"); code != 0 {
		t.Fatalf("status code=%d out=%s", code, out)
	}
}

func TestHooksVerticalSliceRestoresExactBytesAndMode(t *testing.T) {
	profileDir, deps := configSandbox(t)
	hooksDir := t.TempDir()
	deps.HooksDir = func() (string, error) { return hooksDir, nil }
	hook := filepath.Join(hooksDir, "post-update.d", "update-rust")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	source := []byte("#!/bin/bash\nprintf hook\\n\n")
	if err := os.WriteFile(hook, source, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "hooks"); code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	d, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Manifest.Capture.Hooks || len(d.Hooks.Items) != 1 || d.Hooks.Items[0].Mode != "0755" {
		t.Fatalf("hooks=%#v capture=%#v", d.Hooks, d.Manifest.Capture)
	}
	snapshot := filepath.Join(profileDir, "hooks", "files", "post-update.d", "update-rust")
	info, err := os.Stat(snapshot)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("snapshot mode=%v err=%v", info.Mode(), err)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "status", "hooks"); code != 2 || !strings.Contains(out, "- hook post-update.d/update-rust") {
		t.Fatalf("status code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "restore", "hooks", "--dry-run"); code != 0 || !strings.Contains(out, "Omarchy hooks are arbitrary user code") {
		t.Fatalf("dry run code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "restore", "hooks", "--yes"); code != 0 {
		t.Fatalf("restore code=%d out=%s", code, out)
	}
	got, err := os.ReadFile(hook)
	if err != nil || string(got) != string(source) {
		t.Fatalf("hook=%q err=%v", got, err)
	}
	info, err = os.Stat(hook)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("hook mode=%v err=%v", info.Mode(), err)
	}
	if code, out := configRun(t, deps, profileDir, "status", "hooks"); code != 0 {
		t.Fatalf("status code=%d out=%s", code, out)
	}
}

func TestHooksUnmanagedSymlinkWarnsWithoutDrift(t *testing.T) {
	profileDir, deps := configSandbox(t)
	hooksDir := t.TempDir()
	deps.HooksDir = func() (string, error) { return hooksDir, nil }
	target := filepath.Join(t.TempDir(), "external-hook")
	if err := os.WriteFile(target, []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(hooksDir, "theme-set")); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "capture", "hooks"); code != 0 || !strings.Contains(out, "theme-set is a symlink and was left unmanaged") {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "status", "hooks"); code != 0 || !strings.Contains(out, "Profile matches this machine") || !strings.Contains(out, "left unmanaged") {
		t.Fatalf("status code=%d out=%s", code, out)
	}
}

func TestPackagesMiseThreeSourceRestore(t *testing.T) {
	profileDir, deps := configSandbox(t)
	miseConfig := filepath.Join(t.TempDir(), "mise", "config.toml")
	deps.MiseGlobalConfig = func() (string, error) { return miseConfig, nil }
	if err := os.MkdirAll(filepath.Dir(miseConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceConfig := []byte("# source\n[tools]\nnode = \"24\"\n\"npm:@anthropic-ai/claude-code\" = \"latest\"\n")
	if err := os.WriteFile(miseConfig, sourceConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := deps.Runner.(*machineRunner)
	runner.official = map[string]bool{"git": true}
	runner.aur = map[string]bool{"visual-studio-code-bin": true}
	if code, out := configRun(t, deps, profileDir, "capture", "packages"); code != 0 {
		t.Fatalf("capture code=%d out=%s", code, out)
	}
	targetConfig := []byte("# target-only prefix\n[tools]\nbun = \"latest\"\n\n[env]\nKEEP = \"yes\"\n")
	if err := os.WriteFile(miseConfig, targetConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	runner.official, runner.aur = map[string]bool{}, map[string]bool{}
	if code, out := configRun(t, deps, profileDir, "restore", "packages", "--yes"); code != 0 {
		t.Fatalf("restore code=%d out=%s", code, out)
	}
	result, err := os.ReadFile(miseConfig)
	if err != nil || !bytes.HasPrefix(result, targetConfig) || !strings.Contains(string(result), "[tools.node]") || !strings.Contains(string(result), "npm:@anthropic-ai/claude-code") {
		t.Fatalf("config=%s err=%v", result, err)
	}
	if !reflect.DeepEqual(runner.miseCommands, [][]string{{"mise", "-C", "/", "install", "node", "npm:@anthropic-ai/claude-code"}}) {
		t.Fatalf("mise commands=%#v", runner.miseCommands)
	}
	if code, out := configRun(t, deps, profileDir, "status", "packages"); code != 2 || !strings.Contains(out, "mise package bun") {
		t.Fatalf("status code=%d out=%s", code, out)
	}
}

func TestTrackTrackedAndUntrackResources(t *testing.T) {
	profileDir, deps := configSandbox(t)
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	deps.HomeDir = func() (string, error) { return home, nil }
	source := filepath.Join(home, "dotfiles", "deploy")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("echo deploy\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".config", "deploy")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../dotfiles/deploy", link); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "track", source); code != 0 || !strings.Contains(out, "resource deploy") {
		t.Fatalf("track code=%d out=%s", code, out)
	}
	d, err := profile.Load(profileDir)
	if err != nil || !d.Manifest.Capture.Resources || len(d.Resources.Items) != 1 || len(d.Resources.Links) != 1 {
		t.Fatalf("resources=%#v err=%v", d.Resources, err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "restore", "resources", "--yes"); code != 0 {
		t.Fatalf("restore code=%d out=%s", code, out)
	}
	if _, err := os.Lstat(source); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.Readlink(link); err != nil || filepath.IsAbs(raw) {
		t.Fatalf("link=%q err=%v", raw, err)
	}
	if code, out := configRun(t, deps, profileDir, "tracked"); code != 0 || !strings.Contains(out, "deploy") || !strings.Contains(out, "copy") {
		t.Fatalf("tracked code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "untrack", "deploy"); code != 0 || !strings.Contains(out, "resource:deploy") {
		t.Fatalf("untrack code=%d out=%s", code, out)
	}
	if _, err := os.Lstat(source); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, profileDir, "track", link); code != 1 || !strings.Contains(out, "is a symlink") {
		t.Fatalf("symlink track code=%d out=%s", code, out)
	}
}

type gitMachineRunner struct{ machineRunner }

func (r *gitMachineRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "git" {
		return command.SystemRunner{}.Run(ctx, name, args...)
	}
	return r.machineRunner.Run(ctx, name, args...)
}

func TestTrackResourceStrategyAndUntrackedFlags(t *testing.T) {
	profileDir, deps := configSandbox(t)
	home := t.TempDir()
	deps.HomeDir = func() (string, error) { return home, nil }
	deps.Runner = &gitMachineRunner{machineRunner: machineRunner{official: map[string]bool{}, aur: map[string]bool{}}}
	root := filepath.Join(home, "dotfiles")
	for _, args := range [][]string{{"init", root}, {"-C", root, "config", "user.email", "test@example.invalid"}, {"-C", root, "config", "user.name", "Blueprint Test"}} {
		if _, err := deps.Runner.Run(context.Background(), "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	writeAppFile(t, filepath.Join(root, "tracked.txt"), "base\n")
	if _, err := deps.Runner.Run(context.Background(), "git", "-C", root, "add", "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Runner.Run(context.Background(), "git", "-C", root, "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Runner.Run(context.Background(), "git", "-C", root, "remote", "add", "origin", "https://github.com/example/dotfiles.git"); err != nil {
		t.Fatal(err)
	}
	writeAppFile(t, filepath.Join(root, "notes,one.md"), "notes\n")
	writeAppFile(t, filepath.Join(root, "another.txt"), "another\n")

	if code, out := configRun(t, deps, profileDir, "track", root, "--strategy", "git+diff", "--include-untracked", "notes,one.md"); code != 0 {
		t.Fatalf("track git+diff code=%d out=%s", code, out)
	}
	d, err := profile.Load(profileDir)
	if err != nil || len(d.Resources.Items) != 1 || d.Resources.Items[0].Strategy != "git+diff" || len(d.Resources.Items[0].Untracked) != 1 || d.Resources.Items[0].Untracked[0].Path != "notes,one.md" {
		t.Fatalf("saved resources=%#v err=%v", d.Resources, err)
	}
	if code, out := configRun(t, deps, profileDir, "track", root, "--include-untracked", "another.txt"); code != 0 {
		t.Fatalf("add untracked code=%d out=%s", code, out)
	}
	if code, out := configRun(t, deps, profileDir, "track", root, "--exclude-untracked", "notes,one.md"); code != 0 {
		t.Fatalf("remove untracked code=%d out=%s", code, out)
	}
	d, err = profile.Load(profileDir)
	if err != nil || len(d.Resources.Items[0].Untracked) != 1 || d.Resources.Items[0].Untracked[0].Path != "another.txt" {
		t.Fatalf("updated resources=%#v err=%v", d.Resources, err)
	}
	if code, out := configRun(t, deps, profileDir, "track", root, "--strategy", "copy"); code != 0 {
		t.Fatalf("track copy code=%d out=%s", code, out)
	}
	d, err = profile.Load(profileDir)
	if err != nil || d.Resources.Items[0].Strategy != "copy" {
		t.Fatalf("copy resources=%#v err=%v", d.Resources, err)
	}
	for _, args := range [][]string{{"track", root, "--include-untracked", "another.txt"}, {"track", root, "--exclude-untracked", "another.txt"}, {"track", root, "--strategy", "invalid"}, {"track", root, "--id", "other"}} {
		if code, out := configRun(t, deps, profileDir, args...); code != 1 {
			t.Fatalf("args=%v code=%d out=%s", args, code, out)
		}
	}
	writeAppFile(t, filepath.Join(root, "tracked.txt"), "dirty\n")
	if code, out := configRun(t, deps, profileDir, "track", root, "--strategy", "git"); code != 0 || !strings.Contains(out, "strategy: git") || !strings.Contains(out, "Local Git state is not captured by strategy git") || !strings.Contains(out, "Use --strategy git+diff") {
		t.Fatalf("dirty git track code=%d out=%s", code, out)
	}
	for _, args := range [][]string{{"track", root, "--include-untracked", "another.txt"}, {"track", root, "--exclude-untracked", "another.txt"}} {
		if code, out := configRun(t, deps, profileDir, args...); code != 1 {
			t.Fatalf("git args=%v code=%d out=%s", args, code, out)
		}
	}
	if code, out := configRun(t, deps, profileDir, "--json", "track", root, "--strategy", "git"); code != 0 || !strings.Contains(out, "\"strategy\": \"git\"") || strings.Contains(out, "Local Git state is not captured") {
		t.Fatalf("JSON track code=%d out=%s", code, out)
	}
}

func TestRenderResourceProgressUsesResourceLabels(t *testing.T) {
	var out bytes.Buffer
	for _, op := range []model.Operation{{Provider: "resources", Action: "git clone", Resource: "resource:dotfiles"}, {Provider: "resources", Action: "git checkout", Resource: "resource:dotfiles"}, {Provider: "resources", Action: "copy", Resource: "resource:scripts"}, {Provider: "resources", Action: "symlink", Resource: "link:~/.config/nvim"}} {
		renderProgress(&out, restore.Progress{Type: restore.ProgressStarted, Operation: op})
		renderProgress(&out, restore.Progress{Type: restore.ProgressHeartbeat, Operation: op, Elapsed: time.Second})
	}
	got := out.String()
	if strings.Contains(got, "package") || strings.Contains(got, "installing 0") || !strings.Contains(got, "Cloning resource dotfiles") || !strings.Contains(got, "Checking out dotfiles") || !strings.Contains(got, "Restoring resource scripts") || !strings.Contains(got, "Creating link ~/.config/nvim") {
		t.Fatalf("progress=%q", got)
	}
}

func shellCanonical(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
