package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func captureFixture(t *testing.T) (*Provider, *profile.Data, string, string) {
	t.Helper()
	dir := t.TempDir()
	roots := discoveryRoots(t)
	if err := os.MkdirAll(roots.UserConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(roots.UserConfigDir, "backup.service")
	if err := os.WriteFile(path, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := profile.New("test", time.Unix(1, 0))
	if err := profile.Save(dir, d); err != nil {
		t.Fatal(err)
	}
	p := &Provider{Systemd: discoverySystemd{units: []ObservedUnit{observedService("backup.service", path)}}, Roots: roots, ProfileDir: dir}
	return p, &d, dir, path
}

func captureContext(choices map[string]workflow.CaptureDecision) workflow.CaptureContext {
	return workflow.CaptureContext{Targets: choices}
}

func seedManagedDefinition(t *testing.T, p *Provider, d *profile.Data) {
	t.Helper()
	path := filepath.Join(p.ProfileDir, "services", "units", "backup.service")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := content.HashRegularFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Manifest.Capture.Services = true
	d.Services.Units = []profile.ServiceUnit{{Name: "backup.service", Kind: "service", Management: profile.ServiceManagementDefinition, Presence: profile.ServicePresent, Definition: "units/backup.service", DefinitionHash: hash, StartIntent: profile.ServiceStartDisabled}}
}

func TestPlainCaptureDoesNotAdoptNewServiceCandidate(t *testing.T) {
	p, d, dir, _ := captureFixture(t)
	state, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true}}))
	if err != nil || state != nil || d.Manifest.Capture.Services || len(d.Services.Units) != 0 {
		t.Fatalf("plain Capture acquired candidate: state=%+v data=%+v err=%v", state, d.Services, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "services", "units", "backup.service")); !os.IsNotExist(err) {
		t.Fatalf("unreviewed file entered profile: %v", err)
	}
}

func TestReviewedCaptureCanAdoptSelectedDefinition(t *testing.T) {
	p, d, dir, path := captureFixture(t)
	state, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true, Selected: true}}))
	if err != nil || state == nil || !d.Manifest.Capture.Services || len(d.Services.Units) != 1 || d.Services.Units[0].DefinitionHash == "" || d.Services.Units[0].StartIntent != profile.ServiceStartDisabled {
		t.Fatalf("reviewed service was not captured: state=%+v data=%+v err=%v", state, d.Services, err)
	}
	if err := profile.Save(dir, *d); err != nil {
		t.Fatal(err)
	}
	if err := p.CommitCapture(); err != nil {
		t.Fatal(err)
	}
	defer p.FinalizeCapture()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "services", "units", "backup.service"))
	if err != nil || string(got) != string(want) || p.Check(context.Background(), *d) != nil {
		t.Fatalf("authored unit bytes were not preserved: got=%s err=%v", got, err)
	}
}

func TestCapturePreserveFreezesManagedService(t *testing.T) {
	p, d, dir, live := captureFixture(t)
	ctx := captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true, Selected: true}})
	if _, _, err := p.Capture(context.Background(), d, ctx); err != nil {
		t.Fatal(err)
	}
	if err := profile.Save(dir, *d); err != nil {
		t.Fatal(err)
	}
	if err := p.CommitCapture(); err != nil {
		t.Fatal(err)
	}
	if err := p.FinalizeCapture(); err != nil {
		t.Fatal(err)
	}
	oldHash := d.Services.Units[0].DefinitionHash
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: false, Resolved: true}})); err != nil {
		t.Fatal(err)
	}
	if d.Services.Units[0].DefinitionHash != oldHash {
		t.Fatal("Capture Preserve changed managed definition hash")
	}
	if err := p.RollbackCapture(); err != nil {
		t.Fatal(err)
	}
}

func TestCapturePreserveRejectsMissingSnapshotProvenance(t *testing.T) {
	p, d, _, _ := captureFixture(t)
	seedManagedDefinition(t, p, d)
	d.Services.Units[0].DefinitionHash = ""
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: false, Resolved: true}})); err == nil {
		t.Fatal("unverifiable managed definition was preserved as a valid generation")
	}
	if p.prepared != nil {
		t.Fatal("failed preservation left a staged generation")
	}
}

func TestCaptureUpdateRefreshesAlreadyManagedService(t *testing.T) {
	p, d, dir, live := captureFixture(t)
	selected := captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true, Selected: true}})
	if _, _, err := p.Capture(context.Background(), d, selected); err != nil {
		t.Fatal(err)
	}
	if err := profile.Save(dir, *d); err != nil {
		t.Fatal(err)
	}
	if err := p.CommitCapture(); err != nil {
		t.Fatal(err)
	}
	if err := p.FinalizeCapture(); err != nil {
		t.Fatal(err)
	}
	oldHash := d.Services.Units[0].DefinitionHash
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true}})); err != nil || d.Services.Units[0].DefinitionHash == oldHash {
		t.Fatalf("managed definition was not refreshed: %+v err=%v", d.Services, err)
	}
	if err := p.RollbackCapture(); err != nil {
		t.Fatal(err)
	}
}

func TestMissingManagedUnitWithoutReviewedRemovalPreservesDesiredState(t *testing.T) {
	p, d, _, _ := captureFixture(t)
	seedManagedDefinition(t, p, d)
	p.Systemd = discoverySystemd{}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true}})); err != nil || d.Services.Units[0].Presence != profile.ServicePresent {
		t.Fatalf("unreviewed absence replaced desired state: %+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
}

func TestReviewedRemovalRecordsExactDesiredAbsence(t *testing.T) {
	p, d, _, _ := captureFixture(t)
	seedManagedDefinition(t, p, d)
	wantHash := d.Services.Units[0].DefinitionHash
	p.Systemd = discoverySystemd{}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true, Selected: true}})); err != nil || d.Services.Units[0].Presence != profile.ServiceAbsent || d.Services.Units[0].DefinitionHash != wantHash {
		t.Fatalf("reviewed absence lost prior provenance: %+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
}

func TestOverlayCaptureStoresDropInWithoutExternalBase(t *testing.T) {
	p, d, dir, _ := captureFixture(t)
	dropIn := filepath.Join(p.Roots.UserConfigDir, "pipewire.service.d", "10-custom.conf")
	if err := os.MkdirAll(filepath.Dir(dropIn), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropIn, []byte("[Service]\nEnvironment=MODE=custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := observedService("pipewire.service", "/usr/lib/systemd/user/pipewire.service")
	unit.DropInPaths = []string{dropIn}
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true, Selected: true}})); err != nil || len(d.Services.Units) != 1 || d.Services.Units[0].Management != profile.ServiceManagementCustomization || d.Services.Units[0].Definition != "" || len(d.Services.Units[0].DropIns) != 1 {
		t.Fatalf("external base leaked into overlay Capture: %+v err=%v", d.Services, err)
	}
	if err := profile.Save(dir, *d); err != nil {
		t.Fatal(err)
	}
	if err := p.CommitCapture(); err != nil {
		t.Fatal(err)
	}
	defer p.FinalizeCapture()
	if _, err := os.Stat(filepath.Join(dir, "services", "units", "pipewire.service")); !os.IsNotExist(err) {
		t.Fatalf("external base was copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "services", "units", "pipewire.service.d", "10-custom.conf")); err != nil {
		t.Fatal(err)
	}
}

func TestDiffReportsManagedDropInDriftWithoutOwningBase(t *testing.T) {
	p, d, dir, _ := captureFixture(t)
	dropIn := filepath.Join(p.Roots.UserConfigDir, "pipewire.service.d", "10-custom.conf")
	if err := os.MkdirAll(filepath.Dir(dropIn), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropIn, []byte("[Service]\nEnvironment=MODE=first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := observedService("pipewire.service", "/usr/lib/systemd/user/pipewire.service")
	unit.DropInPaths = []string{dropIn}
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true, Selected: true}})); err != nil {
		t.Fatal(err)
	}
	if err := profile.Save(dir, *d); err != nil {
		t.Fatal(err)
	}
	if err := p.CommitCapture(); err != nil {
		t.Fatal(err)
	}
	if err := p.FinalizeCapture(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropIn, []byte("[Service]\nEnvironment=MODE=second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := p.Diff(context.Background(), *d)
	if err != nil || len(changes) != 1 || changes[0].Name != "pipewire.service" {
		t.Fatalf("managed overlay drift vanished: changes=%+v err=%v", changes, err)
	}
}

func TestReviewedRemovalRecordsDropInAbsenceWithoutDeletingExternalBase(t *testing.T) {
	p, d, dir, _ := captureFixture(t)
	dropIn := filepath.Join(p.Roots.UserConfigDir, "pipewire.service.d", "10-custom.conf")
	if err := os.MkdirAll(filepath.Dir(dropIn), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropIn, []byte("[Service]\nEnvironment=MODE=custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := observedService("pipewire.service", "/usr/lib/systemd/user/pipewire.service")
	unit.DropInPaths = []string{dropIn}
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true, Selected: true}})); err != nil {
		t.Fatal(err)
	}
	if err := profile.Save(dir, *d); err != nil {
		t.Fatal(err)
	}
	if err := p.CommitCapture(); err != nil {
		t.Fatal(err)
	}
	if err := p.FinalizeCapture(); err != nil {
		t.Fatal(err)
	}
	wantHash, wantMode := d.Services.Units[0].DropIns[0].Hash, d.Services.Units[0].DropIns[0].Mode
	if err := os.Remove(dropIn); err != nil {
		t.Fatal(err)
	}
	unit.DropInPaths = nil
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	targets, err := p.InspectTargets(context.Background(), *d)
	if err != nil || len(targets) != 1 || !targets[0].RequiresSelection || !targets[0].CaptureEligible {
		t.Fatalf("missing managed drop-in is not reviewable: targets=%+v err=%v", targets, err)
	}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true}})); err != nil || d.Services.Units[0].DropIns[0].Presence != profile.ServicePresent {
		t.Fatalf("unreviewed overlay removal changed desired state: %+v err=%v", d.Services, err)
	}
	if err := p.RollbackCapture(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"pipewire.service": {Capture: true, Resolved: true, Selected: true}})); err != nil || d.Services.Units[0].Definition != "" || d.Services.Units[0].DropIns[0].Presence != profile.ServiceAbsent || d.Services.Units[0].DropIns[0].Hash != wantHash || d.Services.Units[0].DropIns[0].Mode != wantMode {
		t.Fatalf("reviewed overlay removal lost provenance or adopted base: %+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
}

func TestReviewedRemovalRecordsPersistentExternalMaskAbsence(t *testing.T) {
	p, d, dir, _ := captureFixture(t)
	mask := filepath.Join(p.Roots.UserConfigDir, "vendor.service")
	if err := os.Symlink("/dev/null", mask); err != nil {
		t.Fatal(err)
	}
	unit := observedService("vendor.service", "/dev/null")
	unit.RawUnitFileState, unit.StartIntent = "masked", profile.ServiceStartMasked
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"vendor.service": {Capture: true, Resolved: true, Selected: true}})); err != nil {
		t.Fatal(err)
	}
	if err := profile.Save(dir, *d); err != nil {
		t.Fatal(err)
	}
	if err := p.CommitCapture(); err != nil {
		t.Fatal(err)
	}
	if err := p.FinalizeCapture(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mask); err != nil {
		t.Fatal(err)
	}
	unit = observedService("vendor.service", "/usr/lib/systemd/user/vendor.service")
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	targets, err := p.InspectTargets(context.Background(), *d)
	if err != nil || len(targets) != 1 || !targets[0].RequiresSelection || !targets[0].CaptureEligible {
		t.Fatalf("missing managed user mask is not reviewable: targets=%+v err=%v", targets, err)
	}
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"vendor.service": {Capture: true, Resolved: true, Selected: true}})); err != nil || d.Services.Units[0].Mask == nil || d.Services.Units[0].Mask.Presence != profile.ServiceAbsent || d.Services.Units[0].Definition != "" {
		t.Fatalf("reviewed mask removal acquired external base: %+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
}

func TestManagedDefinitionCannotAdoptNewExternalBase(t *testing.T) {
	p, d, _, _ := captureFixture(t)
	seedManagedDefinition(t, p, d)
	unit := observedService("backup.service", "/usr/lib/systemd/user/backup.service")
	p.Systemd = discoverySystemd{units: []ObservedUnit{unit}}
	targets, err := p.InspectTargets(context.Background(), *d)
	if err != nil || len(targets) != 1 || targets[0].CaptureEligible {
		t.Fatalf("external replacement became managed definition: targets=%+v err=%v", targets, err)
	}
	before := d.Services.Units[0].DefinitionHash
	if _, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: false, Resolved: true}})); err != nil || d.Services.Units[0].DefinitionHash != before {
		t.Fatalf("external replacement overwrote managed intent: %+v err=%v", d.Services, err)
	}
	_ = p.RollbackCapture()
}

func TestCaptureRejectsSensitiveUnitBeforeStaging(t *testing.T) {
	p, d, dir, live := captureFixture(t)
	if err := os.WriteFile(live, []byte("[Service]\nEnvironment=api_token=abcdefghijklmnopqrstuvwx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := p.Capture(context.Background(), d, captureContext(map[string]workflow.CaptureDecision{"backup.service": {Capture: true, Resolved: true, Selected: true}}))
	if err == nil || !strings.Contains(err.Error(), "sensitive") || len(d.Services.Units) != 0 {
		t.Fatalf("sensitive Services content was staged: err=%v state=%+v", err, d.Services)
	}
	if _, err := os.Stat(filepath.Join(dir, "services", "units", "backup.service")); !os.IsNotExist(err) {
		t.Fatalf("sensitive file entered profile: %v", err)
	}
}
