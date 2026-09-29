package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type discoverySystemd struct{ units []ObservedUnit }

type unavailableSystemd struct{ discoverySystemd }

func (unavailableSystemd) InspectUserUnits(context.Context) ([]ObservedUnit, error) {
	return nil, errors.New("user manager unavailable")
}

func (d discoverySystemd) InspectUserUnits(context.Context) ([]ObservedUnit, error) {
	return d.units, nil
}
func (discoverySystemd) VerifyUnitSet(context.Context, ProposedUnitSet) error {
	panic("validation is not discovery")
}
func (discoverySystemd) DaemonReload(context.Context) error      { panic("inspection mutated systemd") }
func (discoverySystemd) Enable(context.Context, ...string) error { panic("inspection enabled a unit") }
func (discoverySystemd) Disable(context.Context, ...string) error {
	panic("inspection disabled a unit")
}
func (discoverySystemd) Mask(context.Context, ...string) error   { panic("inspection masked a unit") }
func (discoverySystemd) Unmask(context.Context, ...string) error { panic("inspection unmasked a unit") }
func (discoverySystemd) Start(context.Context, ...string) error  { panic("inspection started a unit") }

func discoveryRoots(t *testing.T) Roots {
	t.Helper()
	home := t.TempDir()
	return Roots{UserConfigDir: filepath.Join(home, ".config", "systemd", "user"), UserDataDir: filepath.Join(home, ".local", "share", "systemd", "user")}
}

func observedService(name, fragment string) ObservedUnit {
	return ObservedUnit{Name: name, Kind: strings.TrimPrefix(filepath.Ext(name), "."), FragmentPath: fragment, TopologyKnown: true, Persistent: true, StartIntent: profile.ServiceStartDisabled}
}

func TestDiscoveryStrongUserConfigIsCandidateNotOwned(t *testing.T) {
	roots := discoveryRoots(t)
	unit := observedService("backup.service", filepath.Join(roots.UserConfigDir, "backup.service"))
	got := Discover([]ObservedUnit{unit}, profile.Services{}, roots)
	if len(got) != 1 || got[0].Provenance != ProvenanceUserConfig || got[0].Management != profile.ServiceManagementDefinition || !got[0].Recommended || !got[0].Eligible || got[0].Managed {
		t.Fatalf("strong custom service silently owned or not offered: %+v", got)
	}
}

func TestDiscoveryUserDataApplicationUnitIsAmbiguousCandidate(t *testing.T) {
	roots := discoveryRoots(t)
	unit := observedService("app-agent.service", filepath.Join(roots.UserDataDir, "app-agent.service"))
	got := Discover([]ObservedUnit{unit}, profile.Services{}, roots)
	if len(got) != 1 || got[0].Provenance != ProvenanceUserDataAmbiguous || got[0].Recommended || !got[0].Advanced || got[0].Managed || !strings.Contains(got[0].Reason, "Application-installed") {
		t.Fatalf("user-data application unit gained implied ownership: %+v", got)
	}
}

func TestDiscoveryPackageBaseIsExternal(t *testing.T) {
	roots := discoveryRoots(t)
	got := Discover([]ObservedUnit{observedService("pipewire.service", "/usr/lib/systemd/user/pipewire.service")}, profile.Services{}, roots)
	if len(got) != 1 || got[0].Provenance != ProvenanceExternal || got[0].Eligible || got[0].Management != "" {
		t.Fatalf("external base was offered as an owned definition: %+v", got)
	}
}

func TestDiscoveryExternalBaseWithUserDropInOffersCustomization(t *testing.T) {
	roots := discoveryRoots(t)
	unit := observedService("pipewire.service", "/usr/lib/systemd/user/pipewire.service")
	unit.DropInPaths = []string{filepath.Join(roots.UserConfigDir, "pipewire.service.d", "10-custom.conf")}
	got := Discover([]ObservedUnit{unit}, profile.Services{}, roots)
	if len(got) != 1 || got[0].Management != profile.ServiceManagementCustomization || !got[0].Eligible || got[0].Managed || !strings.Contains(got[0].Reason, "Managed customization") {
		t.Fatalf("external base/drop-in boundary lost: %+v", got)
	}
}

func TestDiscoveryBroadUserDropInCannotBeRehomedToOneUnit(t *testing.T) {
	roots := discoveryRoots(t)
	unit := observedService("pipewire.service", "/usr/lib/systemd/user/pipewire.service")
	unit.DropInPaths = []string{filepath.Join(roots.UserConfigDir, "service.d", "10-shared.conf")}
	got := Discover([]ObservedUnit{unit}, profile.Services{}, roots)
	if len(got) != 1 || got[0].Eligible || got[0].Provenance != ProvenanceUnknown {
		t.Fatalf("shared drop-in was offered as unit-specific ownership: %+v", got)
	}
}

func TestDiscoveryUserDataDropInCannotBeSilentlyOmitted(t *testing.T) {
	roots := discoveryRoots(t)
	unit := observedService("backup.service", filepath.Join(roots.UserConfigDir, "backup.service"))
	unit.DropInPaths = []string{filepath.Join(roots.UserDataDir, "backup.service.d", "10-app.conf")}
	got := Discover([]ObservedUnit{unit}, profile.Services{}, roots)
	if len(got) != 1 || got[0].Eligible || got[0].Provenance != ProvenanceUnknown {
		t.Fatalf("effective user-data drop-in would be omitted on Capture: %+v", got)
	}
}

func TestDiscoveryInstanceDoesNotDuplicateItsTemplateDefinition(t *testing.T) {
	roots := discoveryRoots(t)
	unit := observedService("backup@photos.service", filepath.Join(roots.UserConfigDir, "backup@.service"))
	unit.InstanceOf = "backup@.service"
	got := Discover([]ObservedUnit{unit}, profile.Services{}, roots)
	if len(got) != 1 || got[0].Eligible || got[0].Provenance != ProvenanceUnknown {
		t.Fatalf("instance was offered a duplicate template definition: %+v", got)
	}
}

func TestDiscoveryPersistentUserMaskOffersCustomization(t *testing.T) {
	roots := discoveryRoots(t)
	if err := os.MkdirAll(roots.UserConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", filepath.Join(roots.UserConfigDir, "vendor-agent.service")); err != nil {
		t.Fatal(err)
	}
	unit := observedService("vendor-agent.service", "/dev/null")
	unit.RawUnitFileState, unit.StartIntent = "masked", profile.ServiceStartMasked
	got := Discover([]ObservedUnit{unit}, profile.Services{}, roots)
	if len(got) != 1 || got[0].Management != profile.ServiceManagementCustomization || !got[0].Eligible || !strings.Contains(got[0].Reason, "Blocked from starting") {
		t.Fatalf("managed user mask confused with external base: %+v", got)
	}
}

func TestDiscoveryGeneratedAndRuntimeUnitsAreExcluded(t *testing.T) {
	roots := discoveryRoots(t)
	generated := observedService("generated.service", "/run/user/1000/systemd/generator/generated.service")
	generated.Generated, generated.Runtime, generated.Persistent = true, true, false
	transient := observedService("transient.service", "/run/user/1000/transient.service")
	transient.Transient, transient.Runtime, transient.Persistent = true, true, false
	got := Discover([]ObservedUnit{generated, transient}, profile.Services{}, roots)
	if len(got) != 0 {
		t.Fatalf("runtime state became a portable candidate: %+v", got)
	}
}

func TestDiscoveryGeneratedUninstantiatedTemplateIsExcluded(t *testing.T) {
	roots := discoveryRoots(t)
	unit := ObservedUnit{Name: "app-agent@.service", Kind: "service", Template: true, RawUnitFileState: "generated", Generated: true, Runtime: true, TopologyKnown: false}
	if got := Discover([]ObservedUnit{unit}, profile.Services{}, roots); len(got) != 0 {
		t.Fatalf("generated catalogue template became a candidate: %+v", got)
	}
}

func TestDiscoveryAdvancedKindsAreMarkedAdvanced(t *testing.T) {
	roots := discoveryRoots(t)
	for _, kind := range []string{"socket", "path", "target", "slice"} {
		name := "watch." + kind
		got := Discover([]ObservedUnit{observedService(name, filepath.Join(roots.UserConfigDir, name))}, profile.Services{}, roots)
		if len(got) != 1 || !got[0].Advanced || got[0].Recommended || !got[0].Eligible {
			t.Errorf("%s did not require advanced selection: %+v", kind, got)
		}
	}
}

func TestDiscoveryScopesDevicesMountsAndUnknownKindsAreExcluded(t *testing.T) {
	roots := discoveryRoots(t)
	var units []ObservedUnit
	for _, kind := range []string{"scope", "device", "mount", "automount", "swap", "unknown"} {
		name := "not-portable." + kind
		units = append(units, observedService(name, filepath.Join(roots.UserConfigDir, name)))
	}
	if got := Discover(units, profile.Services{}, roots); len(got) != 0 {
		t.Fatalf("unsupported unit kinds were offered: %+v", got)
	}
}

func TestDiscoveryLinkedUnitRecordsExternalSource(t *testing.T) {
	roots := discoveryRoots(t)
	unit := observedService("agent.service", filepath.Join(roots.UserConfigDir, "agent.service"))
	unit.RawUnitFileState, unit.LinkedSource = "linked", filepath.Join(t.TempDir(), "agent.service")
	got := Discover([]ObservedUnit{unit}, profile.Services{}, roots)
	if len(got) != 1 || !got[0].Advanced || got[0].Recommended || got[0].Unit.LinkedSource != unit.LinkedSource || !strings.Contains(got[0].Reason, "Definition stored elsewhere") {
		t.Fatalf("linked source relationship became captured bytes: %+v", got)
	}
}

func TestDiscoveryUnresolvedTemplateCannotBeAdopted(t *testing.T) {
	roots := discoveryRoots(t)
	unit := ObservedUnit{Name: "backup@.service", Kind: "service", Template: true, RawUnitFileState: "indirect", StartIntent: profile.ServiceStartIndirect, TopologyKnown: false}
	got := Discover([]ObservedUnit{unit}, profile.Services{}, roots)
	if len(got) != 1 || got[0].Eligible || got[0].Provenance != ProvenanceUnknown || !strings.Contains(got[0].Reason, "not established") {
		t.Fatalf("unresolved template was adopted: %+v", got)
	}
}

func TestDiscoveryTemplateCommentsCannotInventDropIns(t *testing.T) {
	roots := discoveryRoots(t)
	unit := ObservedUnit{Name: "backup@.service", Kind: "service", Template: true, TopologyKnown: false, RawUnitFileState: "static"}
	got := Discover([]ObservedUnit{unit}, profile.Services{}, roots)
	if len(got) != 1 || got[0].Eligible || len(got[0].Unit.DropInPaths) != 0 || got[0].Unit.FragmentPath != "" {
		t.Fatalf("authored comment supplied fake template topology: %+v", got)
	}
}

func TestDiscoveryTemplateAliasesAndBroadDropInsRemainUnknownUntilResolved(t *testing.T) {
	roots := discoveryRoots(t)
	units := []ObservedUnit{{Name: "alias@.service", Kind: "service", Template: true, RawUnitFileState: "alias"}, {Name: "backup@.service", Kind: "service", Template: true, RawUnitFileState: "static"}}
	got := Discover(units, profile.Services{}, roots)
	if len(got) != 2 || got[0].Eligible || got[1].Eligible || got[0].Provenance != ProvenanceUnknown || got[1].Provenance != ProvenanceUnknown {
		t.Fatalf("unresolved alias/hierarchy aborted or gained Capture authority: %+v", got)
	}
}

func TestInspectTargetsFingerprintsExactSelectedSourceChanges(t *testing.T) {
	roots := discoveryRoots(t)
	if err := os.MkdirAll(roots.UserConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(roots.UserConfigDir, "backup.service")
	if err := os.WriteFile(path, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := observedService("backup.service", path)
	p := Provider{Systemd: discoverySystemd{units: []ObservedUnit{unit}}, Roots: roots}
	d := profile.New("test", time.Unix(1, 0))
	first, err := p.InspectTargets(context.Background(), d)
	if err != nil || len(first) != 1 || first[0].Key != "backup.service" || first[0].Current != workflow.TargetPresent || first[0].Desired != workflow.TargetUnknown || !first[0].RequiresSelection || first[0].Fingerprint == "" {
		t.Fatalf("first inspected candidate = %+v err=%v", first, err)
	}
	if err := os.WriteFile(path, []byte("[Service]\nExecStart=/usr/bin/false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := p.InspectTargets(context.Background(), d)
	if err != nil || first[0].Fingerprint == second[0].Fingerprint {
		t.Fatalf("content changed without review fingerprint changing: first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestInspectTargetsFingerprintsManagedDropInChanges(t *testing.T) {
	roots := discoveryRoots(t)
	dropIn := filepath.Join(roots.UserConfigDir, "pipewire.service.d", "10-custom.conf")
	if err := os.MkdirAll(filepath.Dir(dropIn), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropIn, []byte("[Service]\nEnvironment=MODE=first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := observedService("pipewire.service", "/usr/lib/systemd/user/pipewire.service")
	unit.DropInPaths = []string{dropIn}
	p := Provider{Systemd: discoverySystemd{units: []ObservedUnit{unit}}, Roots: roots}
	first, err := p.InspectTargets(context.Background(), profile.New("test", time.Unix(1, 0)))
	if err != nil || len(first) != 1 || !first[0].CaptureEligible {
		t.Fatalf("external customization candidate = %+v err=%v", first, err)
	}
	if err := os.WriteFile(dropIn, []byte("[Service]\nEnvironment=MODE=second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := p.InspectTargets(context.Background(), profile.New("test", time.Unix(1, 0)))
	if err != nil || first[0].Fingerprint == second[0].Fingerprint {
		t.Fatalf("drop-in edit did not change approval fingerprint: first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestInspectTargetsFingerprintsManagedDropInModeChanges(t *testing.T) {
	roots := discoveryRoots(t)
	dropIn := filepath.Join(roots.UserConfigDir, "pipewire.service.d", "10-custom.conf")
	if err := os.MkdirAll(filepath.Dir(dropIn), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropIn, []byte("[Service]\nEnvironment=MODE=custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := observedService("pipewire.service", "/usr/lib/systemd/user/pipewire.service")
	unit.DropInPaths = []string{dropIn}
	p := Provider{Systemd: discoverySystemd{units: []ObservedUnit{unit}}, Roots: roots}
	first, err := p.InspectTargets(context.Background(), profile.New("test", time.Unix(1, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dropIn, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := p.InspectTargets(context.Background(), profile.New("test", time.Unix(1, 0)))
	if err != nil || first[0].Fingerprint == second[0].Fingerprint {
		t.Fatalf("mode changed without invalidating review: first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestInspectTargetsRecommendsOnlyEligibleCustomDependencies(t *testing.T) {
	roots := discoveryRoots(t)
	if err := os.MkdirAll(roots.UserConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"backup.timer", "backup.service"} {
		if err := os.WriteFile(filepath.Join(roots.UserConfigDir, name), []byte("[Unit]\nDescription=backup\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	timer := observedService("backup.timer", filepath.Join(roots.UserConfigDir, "backup.timer"))
	timer.RelatedUnits = []string{"network.target", "backup.service"}
	p := Provider{Systemd: discoverySystemd{units: []ObservedUnit{timer, observedService("backup.service", filepath.Join(roots.UserConfigDir, "backup.service")), observedService("network.target", "/usr/lib/systemd/user/network.target")}}, Roots: roots}
	targets, err := p.InspectTargets(context.Background(), profile.New("test", time.Unix(1, 0)))
	if err != nil || len(targets) != 3 || len(targets[1].RecommendedDependencies) != 1 || targets[1].RecommendedDependencies[0] != "backup.service" {
		t.Fatalf("external dependency gained ownership recommendation: %+v err=%v", targets, err)
	}
}

func TestDiscoveryKeepsMissingManagedUnitVisibleWithoutUnreviewedAbsence(t *testing.T) {
	d := profile.Services{Units: []profile.ServiceUnit{{Name: "backup.service", Kind: "service", Management: profile.ServiceManagementDefinition, Presence: profile.ServicePresent, Definition: "units/backup.service"}}}
	p := Provider{Systemd: discoverySystemd{}, Roots: discoveryRoots(t)}
	data := profile.New("test", time.Unix(1, 0))
	data.Services = d
	targets, err := p.InspectTargets(context.Background(), data)
	if err != nil || len(targets) != 1 || targets[0].Desired != workflow.TargetPresent || targets[0].Current != workflow.TargetAbsent || !targets[0].CaptureEligible || !targets[0].RequiresSelection {
		t.Fatalf("missing managed target was forgotten: %+v err=%v", targets, err)
	}
}

func TestInspectUnavailableUserManagerPreservesUncapturedAndFailsClosedWhenManaged(t *testing.T) {
	p := Provider{Systemd: unavailableSystemd{}, Roots: discoveryRoots(t)}
	data := profile.New("test", time.Unix(1, 0))
	targets, err := p.InspectTargets(context.Background(), data)
	if err != nil || len(targets) != 1 || targets[0].CaptureEligible || !strings.Contains(targets[0].SafetyReason, "unavailable") {
		t.Fatalf("uncaptured Services unavailable state was hidden or fatal: %+v err=%v", targets, err)
	}
	data.Manifest.Capture.Services = true
	data.Services.Units = []profile.ServiceUnit{{Name: "backup.service", Kind: "service", Management: profile.ServiceManagementDefinition, Presence: profile.ServicePresent, Definition: "units/backup.service"}}
	if _, err := p.InspectTargets(context.Background(), data); err == nil {
		t.Fatal("captured Services intent was silently omitted without a user manager")
	}
}
