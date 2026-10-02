package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	services "github.com/Grenco/omarchy-blueprint/internal/providers/services"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestAggregateCaptureDelegatesSystemdUserSourcesToServices(t *testing.T) {
	dir, deps := configSandbox(t)
	miseConfig := filepath.Join(t.TempDir(), "mise.toml")
	deps.MiseGlobalConfig = func() (string, error) { return miseConfig, nil }
	_, configRoot, err := deps.ConfigDirs()
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Dir(configRoot)
	deps.HomeDir = func() (string, error) { return home, nil }
	roots := services.Roots{UserConfigDir: filepath.Join(configRoot, "systemd", "user"), UserDataDir: filepath.Join(home, ".local", "share", "systemd", "user")}
	deps.ServicesRoots = func() services.Roots { return roots }
	deps.IsTTY = func() bool { return true }
	if err := os.MkdirAll(filepath.Join(roots.UserConfigDir, "backup.service.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(roots.UserConfigDir, "backup.service")
	sentinel := filepath.Join(roots.UserConfigDir, "unmanaged.service")
	dropIn := filepath.Join(roots.UserConfigDir, "backup.service.d", "10-custom.conf")
	for path, bytes := range map[string]string{live: "[Service]\nExecStart=/usr/bin/true\n", sentinel: "[Service]\nExecStart=/usr/bin/false\n", dropIn: "[Service]\nEnvironment=MODE=managed\n"} {
		if err := os.WriteFile(path, []byte(bytes), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	actual := appService("backup.service", live)
	actual.DropInPaths = []string{dropIn}
	manager := &appServicesSystemd{units: []services.ObservedUnit{actual, appService("unmanaged.service", sentinel)}}
	deps.ServicesSystemd = manager
	// Explicit Config inclusion must not bypass semantic provider delegation.
	if code, out := configRun(t, deps, dir, "include", "config:.config/systemd/user"); code != 0 {
		t.Fatal(out)
	}
	if code, out := configRun(t, deps, dir, "capture", "config"); code == 0 || !strings.Contains(out, "blocked") {
		t.Fatalf("explicit Config inclusion bypassed Services reservation: code=%d output=%s", code, out)
	}
	data, err := profile.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	data.Config.Included = nil
	if err := profile.Save(dir, data); err != nil {
		t.Fatal(err)
	}
	if code, out := configRun(t, deps, dir, "capture", "config"); code != 0 {
		t.Fatal(out)
	}
	assertNoSystemdConfig := func() {
		t.Helper()
		data, err := profile.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range data.Config.Files {
			if strings.HasPrefix(file.Path, ".config/systemd/user/") {
				t.Fatalf("Config adopted systemd user state: %+v", file)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "config", "files", ".config", "systemd", "user", "unmanaged.service")); !os.IsNotExist(err) {
			t.Fatalf("unselected service entered Config snapshots: %v", err)
		}
	}
	assertNoSystemdConfig()
	deps.In = strings.NewReader("yes\nno\nyes\n")
	if code, out := configRun(t, deps, dir, "capture", "services", "--review"); code != 0 {
		t.Fatal(out)
	}
	data, err = profile.Load(dir)
	if err != nil || len(data.Services.Units) != 1 || data.Services.Units[0].Name != "backup.service" {
		t.Fatalf("reviewed service/sentinel selection wrong: %+v %v", data.Services, err)
	}
	if code, out := configRun(t, deps, dir, "capture"); code != 0 {
		t.Fatal(out)
	}
	assertNoSystemdConfig()
	for _, path := range []string{live, sentinel, dropIn} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	manager.units = nil
	code, out := configRun(t, deps, dir, "--json", "restore", "--dry-run")
	if code != 0 {
		t.Fatal(out)
	}
	var envelope struct {
		Data struct {
			Plan model.RestorePlan `json:"plan"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	plan := envelope.Data.Plan
	writes := map[string]int{}
	for _, op := range plan.Operations {
		if op.File == nil {
			continue
		}
		if strings.HasPrefix(op.File.Destination, roots.UserConfigDir+string(filepath.Separator)) {
			if op.Provider != "services" {
				t.Fatalf("aggregate plan has competing systemd writer: %+v", op)
			}
			writes[op.File.Destination]++
		}
	}
	if writes[live] != 1 || writes[dropIn] != 1 || len(writes) != 2 {
		t.Fatalf("aggregate Services reconstruction duplicates writes or sweeps sentinel: %v", writes)
	}
}

func TestConfigReservesDefaultAndXDGUserServiceRoots(t *testing.T) {
	for _, xdg := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "xdg"}[xdg], func(t *testing.T) {
			dir, deps := configSandbox(t)
			_, configRoot, err := deps.ConfigDirs()
			if err != nil {
				t.Fatal(err)
			}
			home := filepath.Dir(configRoot)
			deps.HomeDir = func() (string, error) { return home, nil }
			deps.PluginDir = func() (string, error) { return filepath.Join(home, ".config", "omarchy", "plugins"), nil }
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("XDG_DATA_HOME", "")
			if xdg {
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "custom-config"))
				t.Setenv("XDG_DATA_HOME", filepath.Join(home, "custom-data"))
			}
			roots := servicesSourceRoots(deps, home)
			provider, err := (configStateProvider{deps: deps, opt: &options{profileDir: dir}}).provider(profile.Data{})
			if err != nil {
				t.Fatal(err)
			}
			for _, root := range []string{roots.UserConfigDir, roots.UserDataDir} {
				for _, relative := range []string{"agent.service", "agent@photos.service.d/10-custom.conf", "timers.target.wants/agent.timer"} {
					claims := provider.Ownership.TrackConflict(filepath.Join(root, relative))
					found := false
					for _, claim := range claims {
						found = found || claim.Provider == "services"
					}
					if !found {
						t.Fatalf("Config can acquire reserved systemd source %s", filepath.Join(root, relative))
					}
				}
			}
		})
	}
}

func TestStaleConfigServiceArtifactCannotGainRestoreAuthority(t *testing.T) {
	dir, deps := configSandbox(t)
	_, configRoot, err := deps.ConfigDirs()
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Dir(configRoot)
	deps.HomeDir = func() (string, error) { return home, nil }
	deps.PluginDir = func() (string, error) { return filepath.Join(home, ".config", "omarchy", "plugins"), nil }
	deps.ServicesRoots = func() services.Roots {
		return services.Roots{UserConfigDir: filepath.Join(configRoot, "systemd", "user"), UserDataDir: filepath.Join(home, ".local", "share", "systemd", "user")}
	}
	data, err := profile.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	logical := ".config/systemd/user/unmanaged.service"
	data.Manifest.Capture.Config = true
	data.Config.Files = []profile.ConfigFile{{Path: logical, Hash: "stale", Mode: "0644"}}
	provider := configStateProvider{deps: deps, opt: &options{profileDir: dir}}
	for _, force := range []bool{false, true} {
		rc := workflow.RestoreContext{Options: policy.DefaultRestoreOptions(), Targets: map[string]workflow.RestoreDecision{logical: {Resolved: true, Restore: false, CompatibilityApply: true}}}
		if force {
			rc.Options.Conflicts = policy.ConflictForce
		}
		// The real target inspection must narrow authority before planning.
		targets, err := provider.InspectTargets(context.Background(), data)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range targets {
			if target.Key == logical && (target.RestoreEligible || target.CaptureEligible) {
				t.Fatalf("saved Config path retained semantic ownership authority: %+v", target)
			}
		}
		fragment, err := provider.Plan(context.Background(), data, omarchy.Info{}, rc)
		if err != nil {
			t.Fatal(err)
		}
		if len(fragment.Operations) != 0 {
			t.Fatalf("Force acquired reserved Services path: force=%v ops=%+v", force, fragment.Operations)
		}
	}
}
