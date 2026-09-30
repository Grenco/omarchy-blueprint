package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	servicesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/services"
	"github.com/Grenco/omarchy-blueprint/internal/tui"
	tuiscreens "github.com/Grenco/omarchy-blueprint/internal/tui/screens"
)

type appServicesSystemd struct {
	units []servicesprovider.ObservedUnit
}

func (s appServicesSystemd) InspectUserUnits(context.Context) ([]servicesprovider.ObservedUnit, error) {
	return s.units, nil
}
func (appServicesSystemd) VerifyUnitSet(context.Context, servicesprovider.ProposedUnitSet) error {
	return errors.New("not part of Capture")
}
func (appServicesSystemd) DaemonReload(context.Context) error       { panic("Capture mutated systemd") }
func (appServicesSystemd) Enable(context.Context, ...string) error  { panic("Capture enabled unit") }
func (appServicesSystemd) Disable(context.Context, ...string) error { panic("Capture disabled unit") }
func (appServicesSystemd) Mask(context.Context, ...string) error    { panic("Capture masked unit") }
func (appServicesSystemd) Unmask(context.Context, ...string) error  { panic("Capture unmasked unit") }
func (appServicesSystemd) Start(context.Context, ...string) error   { panic("Capture started unit") }

func servicesCLIFixture(t *testing.T) (string, Dependencies, servicesprovider.Roots) {
	t.Helper()
	profileDir, deps := configSandbox(t)
	roots := servicesprovider.Roots{UserConfigDir: filepath.Join(t.TempDir(), "systemd", "user"), UserDataDir: filepath.Join(t.TempDir(), "systemd", "user")}
	if err := os.MkdirAll(roots.UserConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	deps.ServicesRoots = func() servicesprovider.Roots { return roots }
	deps.IsTTY = func() bool { return true }
	return profileDir, deps, roots
}

func appService(name, path string) servicesprovider.ObservedUnit {
	return servicesprovider.ObservedUnit{Name: name, Kind: strings.TrimPrefix(filepath.Ext(name), "."), FragmentPath: path, TopologyKnown: true, Persistent: true, StartIntent: profile.ServiceStartDisabled}
}

func TestServicesCLIReviewedCaptureIsOnlyFirstAdoption(t *testing.T) {
	profileDir, deps, roots := servicesCLIFixture(t)
	live := filepath.Join(roots.UserConfigDir, "backup.service")
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps.ServicesSystemd = appServicesSystemd{units: []servicesprovider.ObservedUnit{appService("backup.service", live)}}
	code, preview := configRun(t, deps, profileDir, "--json", "capture", "services", "--review")
	if code != 0 || !strings.Contains(preview, `"outcome": "unselected"`) || !strings.Contains(preview, `"selected": false`) || !strings.Contains(preview, `"backup.service"`) {
		t.Fatalf("JSON review did not show an unselected candidate: code=%d output=%s", code, preview)
	}
	code, output := configRun(t, deps, profileDir, "capture", "services")
	if code != 0 {
		t.Fatalf("plain Capture failed: %s", output)
	}
	prior, err := profile.Load(profileDir)
	if err != nil || prior.Manifest.Capture.Services || len(prior.Services.Units) != 0 {
		t.Fatalf("plain/JSON Capture silently acquired unit: %+v err=%v", prior.Services, err)
	}
	deps.In = strings.NewReader("yes\nyes\n") // candidate choice, then final approval
	code, output = configRun(t, deps, profileDir, "capture", "services", "--review")
	if code != 0 || !strings.Contains(output, "Manage services/backup.service?") || !strings.Contains(output, "Apply this Capture?") {
		t.Fatalf("reviewed Services Capture failed: code=%d output=%s", code, output)
	}
	got, err := profile.Load(profileDir)
	if err != nil || !got.Manifest.Capture.Services || len(got.Services.Units) != 1 || got.Services.Units[0].Name != "backup.service" {
		t.Fatalf("reviewed unit was not persisted: %+v err=%v", got.Services, err)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "services", "units", "backup.service")); err != nil {
		t.Fatal(err)
	}
	if code, output := configRun(t, deps, profileDir, "restore", "services", "--dry-run"); code == 0 || !strings.Contains(output, "persistent Restore and compatibility are not implemented") {
		t.Fatalf("Services Restore claimed premature authority: code=%d output=%s", code, output)
	}
	if code, output := configRun(t, deps, profileDir, "--json", "capture", "services"); code != 0 || !strings.Contains(output, `"services": {`) || !strings.Contains(output, `"backup.service"`) {
		t.Fatalf("managed Services JSON capture output missing: code=%d output=%s", code, output)
	}
}

func TestServicesCLIReviewCanDeclineRecommendedDependency(t *testing.T) {
	profileDir, deps, roots := servicesCLIFixture(t)
	for _, name := range []string{"backup.timer", "backup.service"} {
		if err := os.WriteFile(filepath.Join(roots.UserConfigDir, name), []byte("[Unit]\nDescription=backup\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	timer := appService("backup.timer", filepath.Join(roots.UserConfigDir, "backup.timer"))
	timer.RelatedUnits = []string{"backup.service"}
	deps.ServicesSystemd = appServicesSystemd{units: []servicesprovider.ObservedUnit{timer, appService("backup.service", filepath.Join(roots.UserConfigDir, "backup.service"))}}
	deps.In = strings.NewReader("yes\nno\nyes\n") // parent, recommended child, final approval
	code, output := configRun(t, deps, profileDir, "capture", "services", "--review")
	if code != 0 || !strings.Contains(output, "Manage services/backup.service? [Y/n]") {
		t.Fatalf("dependency recommendation was not reviewable: code=%d output=%s", code, output)
	}
	got, err := profile.Load(profileDir)
	if err != nil || len(got.Services.Units) != 1 || got.Services.Units[0].Name != "backup.timer" {
		t.Fatalf("deselected dependency acquired ownership: %+v err=%v", got.Services, err)
	}
}

func TestServicesCLIDeclinedDependencyStaysDeclinedWhenParentChosenLater(t *testing.T) {
	profileDir, deps, roots := servicesCLIFixture(t)
	names := []string{"backup.timer", "backup.service", "helper-a.service", "helper-b.service"}
	units := make([]servicesprovider.ObservedUnit, 0, len(names))
	for _, name := range names {
		path := filepath.Join(roots.UserConfigDir, name)
		if err := os.WriteFile(path, []byte("[Unit]\nDescription=backup\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		unit := appService(name, path)
		switch name {
		case "backup.timer":
			unit.RelatedUnits = []string{"backup.service"}
		case "backup.service":
			unit.RelatedUnits = []string{"helper-a.service", "helper-b.service"}
		}
		units = append(units, unit)
	}
	deps.ServicesSystemd = appServicesSystemd{units: units}
	deps.In = strings.NewReader("no\nyes\nno\nno\nyes\n") // child, parent, helpers, approval
	code, output := configRun(t, deps, profileDir, "capture", "services", "--review")
	if code != 0 || !strings.Contains(output, "backup.service: Not selected") {
		t.Fatalf("explicitly declined child was reselected: code=%d output=%s", code, output)
	}
	got, err := profile.Load(profileDir)
	if err != nil || len(got.Services.Units) != 1 || got.Services.Units[0].Name != "backup.timer" {
		t.Fatalf("later parent selected the refused child: %+v err=%v", got.Services, err)
	}
}

func TestAggregateCLIReviewLeavesNewServicesUnselected(t *testing.T) {
	profileDir, deps, roots := servicesCLIFixture(t)
	live := filepath.Join(roots.UserConfigDir, "backup.service")
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps.ServicesSystemd = appServicesSystemd{units: []servicesprovider.ObservedUnit{appService("backup.service", live)}}
	deps.PluginDir = func() (string, error) { return t.TempDir(), nil }
	deps.HomeDir = func() (string, error) { return t.TempDir(), nil }
	deps.In = strings.NewReader("yes\n") // the existing final approval prompt only
	code, output := configRun(t, deps, profileDir, "capture", "--review")
	if code != 0 || strings.Contains(output, "Manage services/") || !strings.Contains(output, "Apply this Capture?") {
		t.Fatalf("aggregate review demanded unapproved first-adoption prompts: code=%d output=%s", code, output)
	}
	got, err := profile.Load(profileDir)
	if err != nil || got.Manifest.Capture.Services || len(got.Services.Units) != 0 {
		t.Fatalf("aggregate review acquired an unselected user service: %+v err=%v", got.Services, err)
	}
}

func TestServicesCLIAndTUIShareCandidateTargetIdentity(t *testing.T) {
	profileDir, deps, roots := servicesCLIFixture(t)
	live := filepath.Join(roots.UserConfigDir, "backup.service")
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps.ServicesSystemd = appServicesSystemd{units: []servicesprovider.ObservedUnit{appService("backup.service", live)}}
	if string(tui.ScreenServices) != "services" {
		t.Fatal("TUI Services ID differs from the provider target namespace")
	}
	if code, output := configRun(t, deps, profileDir, "--json", "capture", "services", "--dry-run"); code != 0 || !strings.Contains(output, `"key": "backup.service"`) {
		t.Fatalf("CLI candidate target missing: code=%d output=%s", code, output)
	}
	session, err := openWorkflow(deps, &options{profileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	screen := tuiscreens.NewProvider(session, "services")
	screen.SetSize(100, 22)
	screen.Update(screen.Init()())
	if view := screen.View(); !strings.Contains(view, "backup.service") || !strings.Contains(view, "Custom user service") {
		t.Fatalf("TUI candidate inventory differs from CLI: %s", view)
	}
	if code, output := configRun(t, deps, profileDir, "policy", "show", "services", "backup.service", "--scope", "profile"); code != 0 || !strings.Contains(output, "backup.service") {
		t.Fatalf("Services policy did not resolve target: code=%d output=%s", code, output)
	}
	if code, output := configRun(t, deps, profileDir, "policy", "set", "capture", "services", "preserve", "../other.service", "--scope", "profile"); code == 0 || !strings.Contains(output, "invalid") {
		t.Fatalf("unsafe Services policy target was accepted: code=%d output=%s", code, output)
	}
	if code, output := configRun(t, deps, profileDir, "policy", "set", "capture", "services", "preserve", "backup.service", "--scope", "profile"); code != 0 {
		t.Fatalf("valid Services policy was rejected: %s", output)
	}
	if code, output := configRun(t, deps, profileDir, "--json", "policy", "show", "services", "backup.service", "--scope", "profile"); code != 0 || !strings.Contains(output, `"value": "preserve"`) {
		t.Fatalf("Services target policy lost shared semantics: code=%d output=%s", code, output)
	}
}

func TestServicesCLIStatusAndDiffOnlyReportManagedIntent(t *testing.T) {
	profileDir, deps, roots := servicesCLIFixture(t)
	live := filepath.Join(roots.UserConfigDir, "backup.service")
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps.ServicesSystemd = appServicesSystemd{units: []servicesprovider.ObservedUnit{appService("backup.service", live)}}
	if code, output := configRun(t, deps, profileDir, "status", "services"); code == 0 || !strings.Contains(output, "not been captured") {
		t.Fatalf("uncaptured Services status was successful: code=%d output=%s", code, output)
	}
	deps.In = strings.NewReader("yes\nyes\n")
	if code, output := configRun(t, deps, profileDir, "capture", "services", "--review"); code != 0 {
		t.Fatalf("Capture failed: %s", output)
	}
	if code, output := configRun(t, deps, profileDir, "status", "services"); code != 0 {
		t.Fatalf("newly managed Services status drifted: %s", output)
	}
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, output := configRun(t, deps, profileDir, "diff", "services"); code != 2 || !strings.Contains(output, "backup.service") {
		t.Fatalf("managed Services diff was invisible: code=%d output=%s", code, output)
	}
}
