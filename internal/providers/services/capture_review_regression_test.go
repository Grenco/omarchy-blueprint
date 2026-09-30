package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func seedManagedOverlay(t *testing.T, p *Provider, d *profile.Data) (live string) {
	t.Helper()
	live = filepath.Join(p.Roots.UserConfigDir, "pipewire.service.d", "10-custom.conf")
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("[Service]\nEnvironment=MODE=custom\n")
	if err := os.WriteFile(live, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := content.HashRegularFile(live)
	if err != nil {
		t.Fatal(err)
	}
	artifact := profile.ServiceArtifact{Path: "units/pipewire.service.d/10-custom.conf", Presence: profile.ServicePresent, Hash: hash, Mode: "0644"}
	d.Services.Units = []profile.ServiceUnit{{Name: "pipewire.service", Kind: "service", Management: profile.ServiceManagementCustomization, Presence: profile.ServicePresent, StartIntent: profile.ServiceStartNotManaged, ActivationPreference: profile.ServiceActivationReview, DropIns: []profile.ServiceArtifact{artifact}}}
	d.Manifest.Capture.Services = true
	snapshot := filepath.Join(p.ProfileDir, "services", "units", "pipewire.service.d", "10-custom.conf")
	if err := os.MkdirAll(filepath.Dir(snapshot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return live
}

func TestManagedCustomizationDoesNotAdoptNewBaseWithoutReview(t *testing.T) {
	p, d, dir, _ := captureFixture(t)
	dropIn := seedManagedOverlay(t, p, d)
	newBase := filepath.Join(p.Roots.UserConfigDir, "pipewire.service")
	if err := os.WriteFile(newBase, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := observedService("pipewire.service", newBase)
	unit.DropInPaths = []string{dropIn}
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	targets, err := p.InspectTargets(context.Background(), *d)
	if err != nil || len(targets) != 1 || !targets[0].RequiresSelection {
		t.Fatalf("ownership expansion was not reviewed: targets=%+v err=%v", targets, err)
	}
	before := d.Services.Units[0]
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true}})); err != nil || d.Services.Units[0].Management != profile.ServiceManagementCustomization || d.Services.Units[0].Definition != "" {
		t.Fatalf("plain Capture acquired an external base: state=%+v err=%v", d.Services, err)
	}
	if err := p.RollbackCapture(); err != nil {
		t.Fatal(err)
	}
	if d.Services.Units[0].DropIns[0].Hash != before.DropIns[0].Hash {
		t.Fatal("plain Capture changed the owned overlay while freezing ownership expansion")
	}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true, Selected: true}})); err != nil || d.Services.Units[0].Management != profile.ServiceManagementDefinition || d.Services.Units[0].Definition != "units/pipewire.service" || d.Services.Units[0].DefinitionHash == "" {
		t.Fatalf("reviewed ownership expansion did not capture the new base: state=%+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
	if _, err := os.Stat(filepath.Join(dir, "services", "units", "pipewire.service")); !os.IsNotExist(err) {
		t.Fatalf("reviewed Capture wrote artifacts before workflow commit: %v", err)
	}
}

func TestManagedDefinitionDoesNotDropOwnershipModeWithoutReviewedRemoval(t *testing.T) {
	p, d, _, _ := captureFixture(t)
	seedManagedDefinition(t, p, d)
	unit := observedService("backup.service", filepath.Join(p.Roots.UserConfigDir, "backup.service"))
	unit.LinkedSource = "/home/test/Projects/backup/backup.service"
	unit.RawUnitFileState = "linked"
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	targets, err := p.InspectTargets(context.Background(), *d)
	if err != nil || len(targets) != 1 || targets[0].CaptureEligible {
		t.Fatalf("definition-to-external transition was treated as ordinary update: targets=%+v err=%v", targets, err)
	}
	for _, selected := range []bool{false, true} {
		if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true, Selected: selected}})); err != nil || d.Services.Units[0].Management != profile.ServiceManagementDefinition || d.Services.Units[0].Definition != "units/backup.service" {
			t.Fatalf("linked replacement forgot owned definition: state=%+v selected=%v err=%v", d.Services, selected, err)
		}
		_ = p.RollbackCapture()
	}
}

func TestDiffReportsObservedActiveChange(t *testing.T) {
	p, d, _, live := captureFixture(t)
	seedManagedDefinition(t, p, d)
	unit := observedService("backup.service", live)
	unit.ObservedActive = true
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	changes, err := p.Diff(context.Background(), *d)
	if err != nil || len(changes) != 1 {
		t.Fatalf("Capture would persist active evidence without previewing it: changes=%+v err=%v", changes, err)
	}
}

func TestDiffReportsDropInModeChange(t *testing.T) {
	p, d, _, _ := captureFixture(t)
	dropIn := seedManagedOverlay(t, p, d)
	unit := observedService("pipewire.service", "/usr/lib/systemd/user/pipewire.service")
	unit.DropInPaths = []string{dropIn}
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	if err := os.Chmod(dropIn, 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err := p.Diff(context.Background(), *d)
	if err != nil || len(changes) != 1 {
		t.Fatalf("Capture would persist drop-in mode without previewing it: changes=%+v err=%v", changes, err)
	}
}

func TestDiffReportsLinkedSourceChange(t *testing.T) {
	p, d, _, _ := captureFixture(t)
	d.Services.Units = []profile.ServiceUnit{{Name: "agent.service", Kind: "service", Management: profile.ServiceManagementCustomization, Presence: profile.ServicePresent, StartIntent: profile.ServiceStartNotManaged, LinkedSource: "/home/test/old/agent.service"}}
	unit := observedService("agent.service", filepath.Join(p.Roots.UserConfigDir, "agent.service"))
	unit.RawUnitFileState, unit.LinkedSource = "linked", "/home/test/new/agent.service"
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	changes, err := p.Diff(context.Background(), *d)
	if err != nil || len(changes) != 1 {
		t.Fatalf("Capture would persist a new linked source without preview: changes=%+v err=%v", changes, err)
	}
}

func TestCaptureUpdatePreservesActivationPreference(t *testing.T) {
	p, d, _, live := captureFixture(t)
	seedManagedDefinition(t, p, d)
	d.Services.Units[0].ActivationPreference = profile.ServiceActivationRestoreWorkingState
	unit := observedService("backup.service", live)
	unit.ObservedActive = true
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true}})); err != nil || d.Services.Units[0].ActivationPreference != profile.ServiceActivationRestoreWorkingState || !d.Services.Units[0].ObservedActive {
		t.Fatalf("Capture Update reset activation preference or lost active evidence: state=%+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
}

func TestDiffReportsManagementModeTransition(t *testing.T) {
	p, d, _, _ := captureFixture(t)
	dropIn := seedManagedOverlay(t, p, d)
	base := filepath.Join(p.Roots.UserConfigDir, "pipewire.service")
	if err := os.WriteFile(base, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := observedService("pipewire.service", base)
	unit.DropInPaths = []string{dropIn}
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	changes, err := p.Diff(context.Background(), *d)
	if err != nil || len(changes) != 1 {
		t.Fatalf("ownership scope changed without a factual difference: changes=%+v err=%v", changes, err)
	}
}

func TestCaptureUpdateIncludesNewDropInOnManagedDefinition(t *testing.T) {
	p, d, _, live := captureFixture(t)
	seedManagedDefinition(t, p, d)
	dropIn := filepath.Join(p.Roots.UserConfigDir, "backup.service.d", "20-new-behavior.conf")
	if err := os.MkdirAll(filepath.Dir(dropIn), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropIn, []byte("[Service]\nEnvironment=MODE=new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := observedService("backup.service", live)
	unit.DropInPaths = []string{dropIn}
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	changes, err := p.Diff(context.Background(), *d)
	if err != nil || len(changes) != 1 {
		t.Fatalf("new effective drop-in was hidden from Diff: changes=%+v err=%v", changes, err)
	}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true}})); err != nil || len(d.Services.Units[0].DropIns) != 1 || d.Services.Units[0].DropIns[0].Path != "units/backup.service.d/20-new-behavior.conf" {
		t.Fatalf("managed definition did not capture newly authored drop-in: state=%+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
}

func TestCaptureManagedOverlayNewDropInRequiresReview(t *testing.T) {
	p, d, _, _ := captureFixture(t)
	old := seedManagedOverlay(t, p, d)
	newDropIn := filepath.Join(p.Roots.UserConfigDir, "pipewire.service.d", "20-extra.conf")
	if err := os.WriteFile(newDropIn, []byte("[Service]\nEnvironment=MODE=extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := observedService("pipewire.service", "/usr/lib/systemd/user/pipewire.service")
	unit.DropInPaths = []string{old, newDropIn}
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	targets, err := p.InspectTargets(context.Background(), *d)
	if err != nil || len(targets) != 1 || !targets[0].RequiresSelection || !targets[0].CaptureEligible {
		t.Fatalf("new overlay artifact was not offered for review: targets=%+v err=%v", targets, err)
	}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true}})); err != nil || len(d.Services.Units[0].DropIns) != 1 {
		t.Fatalf("plain Capture silently changed overlay ownership: state=%+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true, Selected: true}})); err != nil || len(d.Services.Units[0].DropIns) != 2 {
		t.Fatalf("reviewed overlay expansion was not recorded: state=%+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
}

func TestCaptureManagedOverlayNewMaskRequiresReview(t *testing.T) {
	p, d, _, _ := captureFixture(t)
	dropIn := seedManagedOverlay(t, p, d)
	maskPath := filepath.Join(p.Roots.UserConfigDir, "pipewire.service")
	if err := os.Symlink("/dev/null", maskPath); err != nil {
		t.Fatal(err)
	}
	unit := observedService("pipewire.service", "/dev/null")
	unit.RawUnitFileState, unit.StartIntent = "masked", profile.ServiceStartMasked
	unit.DropInPaths = []string{dropIn}
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	targets, err := p.InspectTargets(context.Background(), *d)
	if err != nil || len(targets) != 1 || !targets[0].RequiresSelection {
		t.Fatalf("new external-overlay mask did not require review: targets=%+v err=%v", targets, err)
	}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true}})); err != nil || d.Services.Units[0].Mask != nil {
		t.Fatalf("plain Capture silently adopted new mask: state=%+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true, Selected: true}})); err != nil || d.Services.Units[0].Mask == nil || d.Services.Units[0].Mask.Presence != profile.ServicePresent {
		t.Fatalf("reviewed mask ownership was not recorded: state=%+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
}
