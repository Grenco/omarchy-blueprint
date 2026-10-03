package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type planSystemd struct {
	discoverySystemd
	inspectErr, verifyErr error
	// rejectWith, when set, is systemd-analyze output to reject the set
	// with; "<root>" stands for the temporary validation root.
	rejectWith  string
	validations int
	proposed    map[string]string
}

func (s *planSystemd) InspectUserUnits(context.Context) ([]ObservedUnit, error) {
	return s.units, s.inspectErr
}
func (s *planSystemd) VerifyUnitSet(_ context.Context, set ProposedUnitSet) error {
	s.validations++
	s.proposed = map[string]string{}
	err := filepath.WalkDir(set.Root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(set.Root, path)
		if err != nil {
			return err
		}
		s.proposed[filepath.ToSlash(rel)] = string(bytes)
		return nil
	})
	if s.rejectWith != "" {
		return &command.RunError{Name: "env", Output: strings.ReplaceAll(s.rejectWith, "<root>", set.Root), ExitCode: 1, Err: errors.New("exit status 1")}
	}
	return errors.Join(err, s.verifyErr)
}

func persistentPlanFixture(t *testing.T) (*Provider, *profile.Data, *planSystemd, string) {
	t.Helper()
	p, data, _, live := captureFixture(t)
	seedManagedDefinition(t, p, data)
	s := &planSystemd{discoverySystemd: discoverySystemd{units: []ObservedUnit{observedService("backup.service", live)}}}
	p.Systemd = s
	return p, data, s, live
}
func persistentTemplateFixture(t *testing.T) (*Provider, *profile.Data, *planSystemd, string) {
	t.Helper()
	p, data, s, live := persistentPlanFixture(t)
	unit := &data.Services.Units[0]
	snapshot := filepath.Join(p.ProfileDir, "services", filepath.FromSlash(unit.Definition))
	unit.Name, unit.Definition, unit.StartIntent = "backup@.service", "units/backup@.service", profile.ServiceStartIndirect
	if err := os.Rename(snapshot, filepath.Join(p.ProfileDir, "services", filepath.FromSlash(unit.Definition))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.Roots.UserConfigDir, unit.Name)
	if err := os.Rename(live, path); err != nil {
		t.Fatal(err)
	}
	template := observedService(unit.Name, path)
	template.Template, template.StartIntent = true, profile.ServiceStartIndirect
	s.units = []ObservedUnit{template}
	return p, data, s, path
}
func planContext(data profile.Data, force, exact bool) workflow.RestoreContext {
	rc := workflow.RestoreContext{Options: policy.DefaultRestoreOptions(), Targets: map[string]workflow.RestoreDecision{}}
	if force {
		rc.Options.Conflicts = policy.ConflictForce
	}
	if exact {
		rc.Options.Convergence = policy.ConvergenceExact
	}
	for _, unit := range data.Services.Units {
		rc.Targets[unit.Name] = workflow.RestoreDecision{Restore: true, Resolved: true}
	}
	return rc
}
func persistentPlan(t *testing.T, p *Provider, data profile.Data, force, exact bool) workflow.RestoreFragment {
	t.Helper()
	fragment, err := p.Plan(context.Background(), data, omarchy.Info{Version: "4.0.0"}, planContext(data, force, exact))
	if err != nil {
		t.Fatal(err)
	}
	if err := restore.ValidatePlan(model.RestorePlan{Operations: fragment.Operations, Requirements: fragment.Requirements}); err != nil {
		t.Fatal(err)
	}
	return fragment
}
func hasCommand(fragment workflow.RestoreFragment, verb string) bool {
	for _, op := range fragment.Operations {
		if len(op.Command) > 2 && op.Command[0] == "systemctl" && op.Command[1] == "--user" && op.Command[2] == verb {
			return true
		}
	}
	return false
}

func TestPlanCreatesMissingManagedDefinition(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units = nil
	fragment := persistentPlan(t, p, *data, false, false)
	if len(fragment.Operations) < 2 || fragment.Operations[0].File == nil || !fragment.Operations[0].File.ExpectedMissing || !hasCommand(fragment, "daemon-reload") || s.validations != 1 || fragment.Compatibility.State != model.CompatibilitySupported {
		t.Fatalf("missing definition not reconstructed: %+v validations=%d", fragment, s.validations)
	}
	for _, op := range fragment.Operations {
		if hasCommand(workflow.RestoreFragment{Operations: []model.Operation{op}}, "daemon-reload") && len(op.DependsOn) == 0 {
			t.Fatal("daemon-reload did not depend on file writes")
		}
	}
}

func TestPlanRecreatesDefinitionReportedNotFoundByManager(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units = []ObservedUnit{{Name: "backup.service", Kind: "service", LoadState: "not-found"}}
	fragment := persistentPlan(t, p, *data, false, false)
	if len(fragment.Operations) == 0 || fragment.Operations[0].File == nil || !fragment.Operations[0].File.ExpectedMissing {
		t.Fatalf("affirmative absence became unknown: %+v", fragment)
	}
}
func TestPlanSafePreservesUnexpectedDefinitionCollision(t *testing.T) {
	p, data, _, live := persistentPlanFixture(t)
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fragment := persistentPlan(t, p, *data, false, false)
	if len(fragment.Operations) != 0 || len(fragment.Skipped) == 0 || fragment.Compatibility.Authority != model.CompatibilityReduced {
		t.Fatalf("Safe granted conflicting-file authority: %+v", fragment)
	}
}
func TestPlanForceMayReplaceSafeUserOwnedManagedConflict(t *testing.T) {
	p, data, _, live := persistentPlanFixture(t)
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fragment := persistentPlan(t, p, *data, true, false)
	if len(fragment.Operations) == 0 || fragment.Operations[0].File == nil || !fragment.Operations[0].File.Backup || fragment.Operations[0].File.ExpectedExisting == nil {
		t.Fatalf("Force lacks guarded replacement: %+v", fragment)
	}
}
func TestPlanForceCannotReplaceExternalBase(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	external := filepath.Join(t.TempDir(), "backup.service")
	if err := os.WriteFile(external, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.units[0].FragmentPath = external
	fragment := persistentPlan(t, p, *data, true, false)
	if len(fragment.Operations) != 0 || fragment.Compatibility.Authority != model.CompatibilityBlocked {
		t.Fatalf("Force seized external source: %+v live=%s", fragment, live)
	}
}
func TestPlanAdditiveDoesNotApplyDesiredAbsence(t *testing.T) {
	p, data, _, _ := persistentPlanFixture(t)
	data.Services.Units[0].Presence, data.Services.Units[0].StartIntent = profile.ServiceAbsent, profile.ServiceStartNotManaged
	fragment := persistentPlan(t, p, *data, true, false)
	if len(fragment.Operations) != 0 || fragment.Compatibility.Applies {
		t.Fatalf("Additive applied absence: %+v", fragment)
	}
}

func TestPlanExactUnprovedMaskAndInstanceRemovalWithheld(t *testing.T) {
	for _, kind := range []string{"mask", "instance"} {
		t.Run(kind, func(t *testing.T) {
			p, data, s, live := persistentPlanFixture(t)
			if kind == "mask" {
				if err := os.Remove(live); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/dev/null", live); err != nil {
					t.Fatal(err)
				}
				data.Services.Units[0] = profile.ServiceUnit{Name: "backup.service", Kind: "service", Management: profile.ServiceManagementCustomization, Presence: profile.ServicePresent, StartIntent: profile.ServiceStartNotManaged, Mask: &profile.ServiceMask{Presence: profile.ServiceAbsent}}
				s.units[0] = observedService("backup.service", "/dev/null")
				s.units[0].StartIntent, s.units[0].RawUnitFileState = profile.ServiceStartMasked, "masked"
			} else {
				p, data, s, live = persistentTemplateFixture(t)
				data.Services.Units[0].Instances = []profile.ServiceInstance{{Name: "backup@photos.service", Presence: profile.ServiceAbsent, StartIntent: profile.ServiceStartNotManaged}}
				instance := observedService("backup@photos.service", live)
				instance.InstanceOf, instance.StartIntent = "backup@.service", profile.ServiceStartEnabled
				s.units = append(s.units, instance)
			}
			fragment := persistentPlan(t, p, *data, true, true)
			if len(fragment.Operations) != 0 || fragment.Compatibility.Authority != model.CompatibilityReduced {
				t.Fatalf("unproved %s removal retained mutation authority: %+v", kind, fragment)
			}
			wantCode := "services.exact." + kind
			if len(fragment.Compatibility.Findings) != 1 || fragment.Compatibility.Findings[0].Code != wantCode {
				t.Fatalf("unproved %s removal was withheld for the wrong reason: %+v", kind, fragment.Compatibility.Findings)
			}
		})
	}
}
func TestPlanExactAlreadyAbsentInstanceNeedsNoRemovalAuthority(t *testing.T) {
	for _, state := range []string{"not-listed", "disabled"} {
		t.Run(state, func(t *testing.T) {
			p, data, s, live := persistentTemplateFixture(t)
			data.Services.Units[0].Instances = []profile.ServiceInstance{{Name: "backup@photos.service", Presence: profile.ServiceAbsent, StartIntent: profile.ServiceStartNotManaged}}
			if state == "disabled" {
				instance := observedService("backup@photos.service", live)
				instance.InstanceOf = "backup@.service"
				s.units = append(s.units, instance)
			}
			converged := persistentPlan(t, p, *data, false, true)
			if len(converged.Operations) != 0 || converged.Compatibility.State != model.CompatibilitySupported || converged.Compatibility.Authority != model.CompatibilityUnchanged || len(converged.Skipped) != 0 {
				t.Fatalf("converged instance tombstone required removal authority: %+v", converged)
			}
			result, err := p.Verify(context.Background(), *data, planContext(*data, false, true))
			if err != nil || !result.OK {
				t.Fatalf("already-absent instance failed Verify: %+v err=%v", result, err)
			}
			if err := os.Remove(live); err != nil {
				t.Fatal(err)
			}
			// Keep authoritative parent topology but require independent Safe
			// definition work: the instance tombstone must not suppress it.
			validations := s.validations
			fragment := persistentPlan(t, p, *data, false, true)
			if fragment.Compatibility.State != model.CompatibilitySupported || fragment.Compatibility.Authority != model.CompatibilityUnchanged || len(fragment.Skipped) != 0 {
				t.Fatalf("already-absent instance reduced parent authority: %+v", fragment)
			}
			if len(fragment.Operations) != 2 || fragment.Operations[0].File == nil || fragment.Operations[0].File.Destination != live || !fragment.Operations[0].File.ExpectedMissing || !hasCommand(fragment, "daemon-reload") || s.validations != validations+1 {
				t.Fatalf("already-absent instance suppressed independent parent work: %+v", fragment)
			}
		})
	}
}
func TestPlanExactRemovesOnlyExplicitManagedAbsence(t *testing.T) {
	p, data, _, live := persistentPlanFixture(t)
	data.Services.Units[0].Presence, data.Services.Units[0].StartIntent = profile.ServiceAbsent, profile.ServiceStartNotManaged
	other := filepath.Join(filepath.Dir(live), "unmanaged.service")
	if err := os.WriteFile(other, []byte("unmanaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	fragment := persistentPlan(t, p, *data, false, true)
	if len(fragment.Operations) < 2 || fragment.Operations[0].Delete == nil || fragment.Operations[0].Delete.Destination != live || !hasCommand(fragment, "daemon-reload") {
		t.Fatalf("Exact deletion lost bounds: %+v", fragment)
	}
	for _, op := range fragment.Operations {
		if op.Delete != nil && op.Delete.Destination == other {
			t.Fatal("unmanaged deletion planned")
		}
	}
}
func TestPlanExactMissingTombstoneProvenanceWithholdsRemoval(t *testing.T) {
	p, data, _, _ := persistentPlanFixture(t)
	data.Services.Units[0].Presence, data.Services.Units[0].StartIntent, data.Services.Units[0].DefinitionHash = profile.ServiceAbsent, profile.ServiceStartNotManaged, ""
	fragment := persistentPlan(t, p, *data, true, true)
	if len(fragment.Operations) != 0 || fragment.Compatibility.Authority != model.CompatibilityReduced || len(fragment.Skipped) == 0 {
		t.Fatalf("Exact gained authority from missing provenance: %+v", fragment)
	}
}
func TestPlanConvergesEnabledDisabledAndMaskedValues(t *testing.T) {
	for _, tc := range []struct {
		desired, current profile.ServiceStartIntent
		verb             string
	}{{profile.ServiceStartEnabled, profile.ServiceStartDisabled, "enable"}, {profile.ServiceStartDisabled, profile.ServiceStartEnabled, "disable"}, {profile.ServiceStartMasked, profile.ServiceStartDisabled, "mask"}} {
		t.Run(tc.verb, func(t *testing.T) {
			p, data, s, live := persistentPlanFixture(t)
			if tc.desired == profile.ServiceStartMasked {
				data.Services.Units[0].Management, data.Services.Units[0].Definition, data.Services.Units[0].DefinitionHash = profile.ServiceManagementCustomization, "", ""
				data.Services.Units[0].Mask = &profile.ServiceMask{Presence: profile.ServicePresent}
				external := filepath.Join(t.TempDir(), "backup.service")
				bytes, err := os.ReadFile(live)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(external, bytes, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(live); err != nil {
					t.Fatal(err)
				}
				s.units[0].FragmentPath = external
			}
			data.Services.Units[0].StartIntent, s.units[0].StartIntent = tc.desired, tc.current
			fragment := persistentPlan(t, p, *data, false, false)
			if !hasCommand(fragment, tc.verb) || hasCommand(fragment, "start") || hasCommand(fragment, "stop") || hasCommand(fragment, "restart") {
				t.Fatalf("positive persistent intent not planned: %+v live=%s", fragment, live)
			}
		})
	}
}
func TestPlanInvalidProposedUnitBlocksBeforeMutation(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units = nil
	s.verifyErr = errors.New("invalid syntax")
	fragment := persistentPlan(t, p, *data, false, false)
	if fragment.Compatibility.Authority != model.CompatibilityBlocked || len(fragment.Operations) != 0 {
		t.Fatalf("invalid proposed units retained mutation authority: %+v", fragment)
	}
}

func TestPlanRejectedUnitNamesSystemdsReasonWithoutTheValidationRoot(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units = nil
	s.rejectWith = "<root>/backup.service:7: Unknown key 'X' in section [Service], ignoring.\nbackup.service: Command /usr/bin/restic is not executable: No such file or directory\n"
	fragment := persistentPlan(t, p, *data, false, false)
	if fragment.Compatibility.Authority != model.CompatibilityBlocked || len(fragment.Compatibility.Findings) == 0 {
		t.Fatalf("rejected unit did not block: %+v", fragment.Compatibility)
	}
	finding := fragment.Compatibility.Findings[0]
	if finding.Code != "services.unit.invalid" || finding.Target != "backup.service" || !strings.Contains(finding.Summary, "Command /usr/bin/restic is not executable") || strings.Contains(finding.Summary, "blueprint-services-verify") || strings.Contains(finding.Summary, "Unknown key") {
		t.Fatalf("finding does not carry systemd's reason cleanly: %+v", finding)
	}
}

func TestCompatibilityUnavailableVerifierIsUnknownNotIncompatible(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	s.verifyErr = &ValidationUnavailableError{Err: errors.New("tool missing")}
	fragment := persistentPlan(t, p, *data, false, false)
	if fragment.Compatibility.State != model.CompatibilityUnknown || fragment.Compatibility.Authority != model.CompatibilityBlocked || len(fragment.Requirements) != 1 || len(fragment.Operations) != 0 {
		t.Fatalf("missing verifier was mistaken for incompatible content: %+v", fragment)
	}
}
func TestPlanUnavailableUserManagerCreatesRequirement(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	s.inspectErr = errors.New("cannot connect to user bus")
	fragment := persistentPlan(t, p, *data, false, false)
	if len(fragment.Requirements) != 1 || fragment.Compatibility.Authority != model.CompatibilityBlocked || len(fragment.Compatibility.Findings) == 0 || fragment.Compatibility.Findings[0].RequirementID != fragment.Requirements[0].ID || len(fragment.Operations) != 0 {
		t.Fatalf("user manager unavailable not represented: %+v", fragment)
	}
}
func TestPlanRestoreSkipHasNoServicesAuthorityOrValidation(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	rc := planContext(*data, true, true)
	rc.Targets["backup.service"] = workflow.RestoreDecision{Resolved: true, Reason: "restore disabled"}
	fragment, err := p.Plan(context.Background(), *data, omarchy.Info{}, rc)
	if err != nil || fragment.Compatibility.Applies || len(fragment.Operations) != 0 || s.validations != 0 || len(fragment.Skipped) != 1 {
		t.Fatalf("Skip gained intent: %+v err=%v", fragment, err)
	}
}
func TestPlanMissingExternalBaseBlocksAffectedOverlay(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	data.Services.Units[0] = profile.ServiceUnit{Name: "pipewire.service", Kind: "service", Management: profile.ServiceManagementCustomization, Presence: profile.ServicePresent, StartIntent: profile.ServiceStartEnabled}
	s.units = nil
	fragment := persistentPlan(t, p, *data, true, false)
	if fragment.Compatibility.Authority != model.CompatibilityBlocked || len(fragment.Operations) != 0 || !strings.Contains(fragment.Compatibility.Findings[0].Code, "base") {
		t.Fatalf("missing external base treated as reconstructable: %+v", fragment)
	}
}

func TestPlanUpdatesManagedDropInAndNeverCopiesExternalBase(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	live := seedManagedOverlay(t, p, data)
	external := filepath.Join(t.TempDir(), "pipewire.service")
	base := "[Service]\nExecStart=/usr/bin/true\n"
	if err := os.WriteFile(external, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	actual := observedService("pipewire.service", external)
	actual.DropInPaths = []string{live}
	s.units = []ObservedUnit{actual}
	if err := os.WriteFile(live, []byte("[Service]\nEnvironment=MODE=target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fragment := persistentPlan(t, p, *data, true, false)
	if len(fragment.Operations) < 2 || fragment.Operations[0].File == nil || fragment.Operations[0].File.Destination != live || s.proposed["pipewire.service"] != base || s.proposed["pipewire.service.d/10-custom.conf"] == "" {
		t.Fatalf("overlay proposed set/authority wrong: %+v proposed=%v", fragment, s.proposed)
	}
	for _, op := range fragment.Operations {
		if op.File != nil && op.File.Destination == external || op.Delete != nil && op.Delete.Destination == external {
			t.Fatal("external base mutated")
		}
	}
	bytes, err := os.ReadFile(external)
	if err != nil || string(bytes) != base {
		t.Fatalf("planning changed external base: %q err=%v", bytes, err)
	}
}
func TestCompatibilityUnknownDependenciesWithholdExactRemoval(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	data.Services.Units[0].Presence, data.Services.Units[0].StartIntent = profile.ServiceAbsent, profile.ServiceStartNotManaged
	s.units[0].RelatedUnits = []string{"unknown-a.service", "unknown-b.service"}
	fragment := persistentPlan(t, p, *data, true, true)
	if len(fragment.Operations) != 0 || fragment.Compatibility.State != model.CompatibilityUnknown || fragment.Compatibility.Authority != model.CompatibilityReduced || len(fragment.Skipped) == 0 {
		t.Fatalf("unknown dependencies retained Exact authority: %+v", fragment)
	}
}
func TestPlanLinkedSourceRequiresExistingSourceAndCoversItsIdentity(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units = nil
	source := filepath.Join(t.TempDir(), "backup.service")
	data.Services.Units[0] = profile.ServiceUnit{Name: "backup.service", Kind: "service", Management: profile.ServiceManagementCustomization, Presence: profile.ServicePresent, StartIntent: profile.ServiceStartDisabled, LinkedSource: source}
	missing := persistentPlan(t, p, *data, false, false)
	if missing.Compatibility.Authority != model.CompatibilityBlocked || len(missing.Operations) != 0 {
		t.Fatalf("missing linked source created dangling link: %+v", missing)
	}
	if err := os.WriteFile(source, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := persistentPlan(t, p, *data, false, false)
	if len(first.Operations) < 2 || first.Operations[0].Symlink == nil || first.Operations[0].Symlink.Target != source {
		t.Fatalf("existing linked source not safely planned: %+v", first)
	}
	identity := ""
	for _, e := range first.Compatibility.Evidence {
		if e.Kind == "services.proposed-set-identity" {
			identity = e.Summary
		}
	}
	if err := os.WriteFile(source, []byte("[Service]\nExecStart=/usr/bin/false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := persistentPlan(t, p, *data, false, false)
	for _, e := range second.Compatibility.Evidence {
		if e.Kind == "services.proposed-set-identity" && e.Summary == identity {
			t.Fatal("changed linked source retained approval identity")
		}
	}
}
func TestPlanTemplateAndConfiguredInstanceKeepOnePersistentDefinition(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	snapshot := filepath.Join(p.ProfileDir, "services", "units", "backup@.service")
	bytes := []byte("[Service]\nExecStart=/usr/bin/true\n")
	if err := os.WriteFile(snapshot, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := readServiceFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	data.Services.Units = []profile.ServiceUnit{{Name: "backup@.service", Kind: "service", Management: profile.ServiceManagementDefinition, Presence: profile.ServicePresent, Definition: "units/backup@.service", DefinitionHash: file.hash, StartIntent: profile.ServiceStartIndirect, Instances: []profile.ServiceInstance{{Name: "backup@photos.service", Presence: profile.ServicePresent, StartIntent: profile.ServiceStartEnabled}}}}
	s.units = nil
	fragment := persistentPlan(t, p, *data, false, false)
	writes := 0
	for _, op := range fragment.Operations {
		if op.File != nil {
			writes++
			if filepath.Base(op.File.Destination) != "backup@.service" {
				t.Fatal("template bytes duplicated as persistent instance")
			}
		}
	}
	if writes != 1 || !hasCommand(fragment, "enable") || s.proposed["backup@photos.service"] == "" {
		t.Fatalf("template/instance intent not planned semantically: %+v proposed=%v", fragment, s.proposed)
	}
}
func TestPlanChangedRunningServiceNeverRestarts(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	s.units[0].ObservedActive = true
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fragment := persistentPlan(t, p, *data, true, false)
	if hasCommand(fragment, "start") || hasCommand(fragment, "stop") || hasCommand(fragment, "restart") || hasCommand(fragment, "reload") {
		t.Fatalf("persistent Restore gained process authority: %+v", fragment)
	}
	warned := false
	for _, skip := range fragment.Skipped {
		warned = warned || strings.Contains(skip.Reason, "will not restart")
	}
	if !warned {
		t.Fatalf("changed running process lacks warning: %+v", fragment)
	}
}
func TestCompatibilityUnresolvedExistingTemplateWithholdsMutation(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	data.Services.Units[0].Name = "backup@.service"
	data.Services.Units[0].Definition = "units/backup@.service"
	s.units = []ObservedUnit{{Name: "backup@.service", Kind: "service", Template: true, RawUnitFileState: "static"}}
	fragment := persistentPlan(t, p, *data, true, true)
	if fragment.Compatibility.State != model.CompatibilityUnknown || fragment.Compatibility.Authority != model.CompatibilityReduced || len(fragment.Operations) != 0 {
		t.Fatalf("unresolved naked template gained authority: %+v", fragment)
	}
}
