package profile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestServicesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	definition := "[Unit]\nDescription=Back up photos\n\n[Service]\nExecStart=/usr/bin/true\n"
	path := filepath.Join(dir, "services", "units", "backup.service")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(definition), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "services", "units", "backup.timer"), []byte("[Timer]\nOnCalendar=daily\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New("test", time.Unix(1, 0))
	d.Manifest.Capture.Services = true
	d.Services.Units = []ServiceUnit{
		{Name: "backup.timer", Kind: "timer", Management: ServiceManagementDefinition, Presence: ServicePresent, StartIntent: ServiceStartDisabled, ActivationPreference: ServiceActivationPersistentOnly, Definition: "units/backup.timer"},
		{Name: "backup.service", Kind: "service", Management: ServiceManagementDefinition, Presence: ServicePresent, StartIntent: ServiceStartEnabled, ActivationPreference: ServiceActivationRestoreWorkingState, ObservedActive: true, Definition: "units/backup.service"},
	}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Schema != 14 || !got.Manifest.Capture.Services || len(got.Services.Units) != 2 || got.Services.Units[0].Name != "backup.service" || got.Services.Units[0].StartIntent != ServiceStartEnabled || !got.Services.Units[0].ObservedActive || got.Services.Units[1].StartIntent != ServiceStartDisabled {
		t.Fatalf("Services round trip = %+v", got)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != definition {
		t.Fatalf("managed authored content changed: %q err=%v", content, err)
	}
	metadata, err := os.ReadFile(filepath.Join(dir, "services", "services.toml"))
	if err != nil || strings.Index(string(metadata), "backup.service") >= strings.Index(string(metadata), "backup.timer") {
		t.Fatalf("Services metadata is missing or not deterministic: %q err=%v", metadata, err)
	}
}

func TestServicesExternalCustomizationDoesNotRequireBaseDefinition(t *testing.T) {
	dir := t.TempDir()
	dropIn := filepath.Join(dir, "services", "units", "pipewire.service.d", "10-custom.conf")
	content := "[Service]\n# Preserve this comment and order\nEnvironment=PIPEWIRE_LATENCY=128/48000\n"
	if err := os.MkdirAll(filepath.Dir(dropIn), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropIn, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New("test", time.Unix(1, 0))
	d.Manifest.Capture.Services = true
	d.Services.Units = []ServiceUnit{{Name: "pipewire.service", Kind: "service", Management: ServiceManagementCustomization, Presence: ServicePresent, StartIntent: ServiceStartNotManaged, ActivationPreference: ServiceActivationPersistentOnly,
		DropIns: []ServiceArtifact{{Path: "units/pipewire.service.d/10-custom.conf", Presence: ServicePresent, Hash: "captured"}}}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || !reflect.DeepEqual(got.Services.Units, d.Services.Units) {
		t.Fatalf("overlay-only Services state = %+v err=%v", got.Services, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "services", "units", "pipewire.service")); !os.IsNotExist(err) {
		t.Fatalf("external base was copied or required in profile: %v", err)
	}
	if after, err := os.ReadFile(dropIn); err != nil || string(after) != content {
		t.Fatalf("managed drop-in bytes were changed: %q err=%v", after, err)
	}
}

func TestServicesExternalMaskAbsenceDoesNotAcquireBase(t *testing.T) {
	dir := t.TempDir()
	d := New("test", time.Unix(1, 0))
	d.Manifest.Capture.Services = true
	d.Services.Units = []ServiceUnit{{Name: "vendor-agent.service", Kind: "service", Management: ServiceManagementCustomization, Presence: ServicePresent, StartIntent: ServiceStartNotManaged, Mask: &ServiceMask{Presence: ServiceAbsent}}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || len(got.Services.Units) != 1 || got.Services.Units[0].Mask == nil || got.Services.Units[0].Mask.Presence != ServiceAbsent || got.Services.Units[0].Definition != "" {
		t.Fatalf("mask absence acquired an external base: %+v err=%v", got.Services, err)
	}
}

func TestServicesTemplateDefinitionStoredOnce(t *testing.T) {
	dir := t.TempDir()
	d := New("test", time.Unix(1, 0))
	d.Services.Units = []ServiceUnit{{Name: "backup@.service", Kind: "service", Management: ServiceManagementDefinition, Presence: ServicePresent, Definition: "units/backup@.service", StartIntent: ServiceStartIndirect,
		Instances: []ServiceInstance{{Name: "backup@projects.service", Presence: ServicePresent, StartIntent: ServiceStartDisabled}, {Name: "backup@photos.service", Presence: ServicePresent, StartIntent: ServiceStartEnabled}}}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || len(got.Services.Units) != 1 || got.Services.Units[0].Definition != "units/backup@.service" || len(got.Services.Units[0].Instances) != 2 || got.Services.Units[0].Instances[0].Name != "backup@photos.service" {
		t.Fatalf("template/instance metadata = %+v err=%v", got.Services, err)
	}
}

func TestServicesDesiredAbsenceCannotAlsoBePresent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		units []ServiceUnit
	}{
		{"definition", []ServiceUnit{{Name: "backup.service", Management: ServiceManagementDefinition, Presence: ServicePresent}, {Name: "backup.service", Management: ServiceManagementDefinition, Presence: ServiceAbsent}}},
		{"drop-in", []ServiceUnit{{Name: "backup.service", Management: ServiceManagementDefinition, Presence: ServicePresent, Definition: "units/backup.service", DropIns: []ServiceArtifact{{Path: "units/backup.service.d/10-custom.conf", Presence: ServicePresent}, {Path: "units/backup.service.d/10-custom.conf", Presence: ServiceAbsent}}}}},
		{"instance", []ServiceUnit{{Name: "backup@.service", Management: ServiceManagementDefinition, Presence: ServicePresent, Definition: "units/backup@.service", Instances: []ServiceInstance{{Name: "backup@photos.service", Presence: ServicePresent}, {Name: "backup@photos.service", Presence: ServiceAbsent}}}}},
		{"external-base", []ServiceUnit{{Name: "pipewire.service", Management: ServiceManagementCustomization, Presence: ServicePresent, Definition: "units/pipewire.service"}}},
		{"external-base-absence", []ServiceUnit{{Name: "pipewire.service", Management: ServiceManagementCustomization, Presence: ServiceAbsent, StartIntent: ServiceStartNotManaged}}},
		{"mask-conflict", []ServiceUnit{{Name: "pipewire.service", Management: ServiceManagementCustomization, Presence: ServicePresent, StartIntent: ServiceStartMasked, Mask: &ServiceMask{Presence: ServiceAbsent}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := New("test", time.Unix(1, 0))
			d.Services.Units = tc.units
			if err := Save(t.TempDir(), d); err == nil {
				t.Fatal("accepted contradictory or externally owned Services state")
			}
		})
	}
}

func TestServicesRejectsUnownedOrEscapingArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name string
		unit ServiceUnit
	}{
		{"missing-managed-definition", ServiceUnit{Name: "backup.service", Kind: "service", Management: ServiceManagementDefinition, Presence: ServicePresent}},
		{"definition-traversal", ServiceUnit{Name: "backup.service", Kind: "service", Management: ServiceManagementDefinition, Presence: ServicePresent, Definition: "units/../outside.service"}},
		{"drop-in-traversal", ServiceUnit{Name: "backup.service", Kind: "service", Management: ServiceManagementDefinition, Presence: ServicePresent, Definition: "units/backup.service", DropIns: []ServiceArtifact{{Path: "units/backup.service.d/../../other.conf", Presence: ServicePresent}}}},
		{"unsupported-kind", ServiceUnit{Name: "backup.mount", Kind: "mount", Management: ServiceManagementDefinition, Presence: ServicePresent, Definition: "units/backup.mount"}},
		{"invalid-start", ServiceUnit{Name: "backup.service", Kind: "service", Management: ServiceManagementDefinition, Presence: ServicePresent, Definition: "units/backup.service", StartIntent: "enabled-runtime"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := New("test", time.Unix(1, 0))
			d.Services.Units = []ServiceUnit{tc.unit}
			if err := Save(t.TempDir(), d); err == nil {
				t.Fatal("accepted non-portable Services state")
			}
		})
	}
}

func TestServicesTemplateInstanceCannotAlsoBeTopLevelUnit(t *testing.T) {
	d := New("test", time.Unix(1, 0))
	d.Services.Units = []ServiceUnit{
		{Name: "backup@.service", Kind: "service", Management: ServiceManagementDefinition, Presence: ServicePresent, Definition: "units/backup@.service", Instances: []ServiceInstance{{Name: "backup@photos.service", Presence: ServicePresent}}},
		{Name: "backup@photos.service", Kind: "service", Management: ServiceManagementCustomization, Presence: ServicePresent},
	}
	if err := Save(t.TempDir(), d); err == nil {
		t.Fatal("same instance was owned twice")
	}
}

func TestSchema13ProfileLoadsWithEmptyServices(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, New("legacy", time.Unix(1, 0))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "profile.toml")
	manifest, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(string(manifest), "schema = 14", "schema = 13", 1)
	if legacy == string(manifest) {
		t.Fatalf("new profile did not use schema 14: %s", manifest)
	}
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "services", "services.toml")); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || got.Manifest.Schema != Schema || got.Manifest.Capture.Services || len(got.Services.Units) != 0 {
		t.Fatalf("schema-13 profile changed Services intent: %+v err=%v", got, err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != legacy {
		t.Fatalf("legacy profile was rewritten on load: %s err=%v", after, err)
	}
}
