package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestVerifyDoesNotRequireRuntimeActiveForPersistentOnlyIntent(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	data.Services.Units[0].ObservedActive = true
	s.units[0].ObservedActive = false
	result, err := p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || !result.OK || s.validations != 1 {
		t.Fatalf("runtime inactivity failed persistent Verify: %+v err=%v", result, err)
	}
}

func TestDefinitionWithoutRecordedModePreservesExistingPermissions(t *testing.T) {
	p, data, _, live := persistentPlanFixture(t)
	if err := os.Chmod(live, 0o600); err != nil {
		t.Fatal(err)
	}
	fragment := persistentPlan(t, p, *data, false, false)
	if len(fragment.Operations) != 0 {
		t.Fatalf("unrecorded definition mode became convergence authority: %+v", fragment)
	}
	result, err := p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || !result.OK {
		t.Fatalf("matching private definition falsely drifted: %+v err=%v", result, err)
	}
}

func TestVerifyManagerLoadErrorDoesNotFalselyConverge(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	s.units[0].LoadState = "error"
	result, err := p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || result.OK {
		t.Fatalf("manager load error was hidden: %+v err=%v", result, err)
	}
	fragment := persistentPlan(t, p, *data, false, false)
	if !hasCommand(fragment, "daemon-reload") || hasCommand(fragment, "restart") {
		t.Fatalf("valid persistent content did not receive an explicit definition refresh: %+v", fragment)
	}
}
func TestVerifyExactDesiredAbsenceWithheldIntentRemainsMissing(t *testing.T) {
	p, data, _, live := persistentPlanFixture(t)
	data.Services.Units[0].Presence, data.Services.Units[0].StartIntent, data.Services.Units[0].DefinitionHash = profile.ServiceAbsent, profile.ServiceStartNotManaged, ""
	fragment := persistentPlan(t, p, *data, true, true)
	if len(fragment.Operations) != 0 {
		t.Fatal("unproved Exact removal planned")
	}
	result, err := p.Verify(context.Background(), *data, planContext(*data, true, true))
	if err != nil || result.OK || len(result.Missing) != 1 {
		t.Fatalf("withheld removal falsely converged: %+v err=%v", result, err)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	result, err = p.Verify(context.Background(), *data, planContext(*data, true, true))
	if err != nil || !result.OK {
		t.Fatalf("already-absent target required deletion authority: %+v err=%v", result, err)
	}
}
func TestVerifyIgnoresUnrelatedUserUnitsAndRestoreSkip(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	s.units = append(s.units, observedService("unrelated.service", "/external/unrelated.service"))
	result, err := p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || !result.OK {
		t.Fatalf("unrelated user unit affected convergence: %+v err=%v", result, err)
	}
	rc := planContext(*data, false, false)
	rc.Targets["backup.service"] = workflow.RestoreDecision{Resolved: true}
	s.inspectErr = os.ErrPermission
	result, err = p.Verify(context.Background(), *data, rc)
	if err != nil || !result.OK {
		t.Fatalf("policy Skip was verified as Apply: %+v err=%v", result, err)
	}
}
func TestVerifyEnabledDisabledAndMaskedIntent(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	data.Services.Units[0].StartIntent = profile.ServiceStartEnabled
	result, err := p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || result.OK {
		t.Fatalf("disabled target satisfied enabled intent: %+v err=%v", result, err)
	}
	s.units[0].StartIntent = profile.ServiceStartEnabled
	result, err = p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || !result.OK {
		t.Fatalf("enabled target failed convergence: %+v err=%v", result, err)
	}
	data.Services.Units[0] = profile.ServiceUnit{Name: "backup.service", Kind: "service", Management: profile.ServiceManagementCustomization, Presence: profile.ServicePresent, StartIntent: profile.ServiceStartMasked, Mask: &profile.ServiceMask{Presence: profile.ServicePresent}}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", filepath.Join(p.Roots.UserConfigDir, "backup.service")); err != nil {
		t.Fatal(err)
	}
	s.units[0] = observedService("backup.service", "/dev/null")
	s.units[0].StartIntent, s.units[0].RawUnitFileState = profile.ServiceStartMasked, "masked"
	result, err = p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || !result.OK {
		t.Fatalf("persistent user mask failed convergence: %+v err=%v", result, err)
	}
}
func TestVerifyDefinitionAndDropInsIncludesModes(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	path := seedManagedOverlay(t, p, data)
	base := filepath.Join(t.TempDir(), "pipewire.service")
	if err := os.WriteFile(base, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	actual := observedService("pipewire.service", base)
	actual.DropInPaths = []string{path}
	s.units = []ObservedUnit{actual}
	result, err := p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || !result.OK {
		t.Fatalf("matching external overlay failed: %+v err=%v", result, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || result.OK {
		t.Fatalf("drop-in mode drift falsely converged: %+v err=%v", result, err)
	}
}

func TestVerifyTemplateInstanceIntentDoesNotDuplicateDefinition(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	path := filepath.Join(p.Roots.UserConfigDir, "backup@.service")
	if err := os.WriteFile(path, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := readServiceFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data.Services.Units = []profile.ServiceUnit{{Name: "backup@.service", Kind: "service", Management: profile.ServiceManagementDefinition, Presence: profile.ServicePresent, Definition: "units/backup@.service", DefinitionHash: file.hash, StartIntent: profile.ServiceStartIndirect, Instances: []profile.ServiceInstance{{Name: "backup@photos.service", Presence: profile.ServicePresent, StartIntent: profile.ServiceStartEnabled}}}}
	instance := observedService("backup@photos.service", path)
	instance.InstanceOf = "backup@.service"
	instance.StartIntent = profile.ServiceStartEnabled
	s.units = []ObservedUnit{{Name: "backup@.service", Kind: "service", Template: true, StartIntent: profile.ServiceStartIndirect}, instance}
	result, err := p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || !result.OK {
		t.Fatalf("template/instance persistent Verify failed: %+v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(p.Roots.UserConfigDir, "backup@photos.service")); !os.IsNotExist(err) {
		t.Fatalf("Verify created duplicate definition: %v", err)
	}
	previous := s.units[1].FragmentPath
	s.units[1].FragmentPath = "/external/backup@photos.service"
	result, err = p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || result.OK {
		t.Fatalf("external instance override falsely converged: %+v err=%v", result, err)
	}
	s.units[1].FragmentPath = previous
	s.units[1].StartIntent = profile.ServiceStartDisabled
	result, err = p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || result.OK {
		t.Fatalf("disabled configured instance falsely converged: %+v err=%v", result, err)
	}
}
