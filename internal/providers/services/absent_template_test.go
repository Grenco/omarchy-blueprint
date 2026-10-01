package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func capturedAbsentTemplateFixture(t *testing.T) (*Provider, *profile.Data, *planSystemd, string) {
	t.Helper()
	p, data, s, live := persistentTemplateFixture(t)
	name := data.Services.Units[0].Name
	template := s.units[0]
	data.Services.Units[0].Instances = []profile.ServiceInstance{{Name: "backup@photos.service", Presence: profile.ServicePresent, StartIntent: profile.ServiceStartEnabled}}
	bytes, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units = nil
	if _, _, err := p.Capture(context.Background(), data, captureContext(map[string]workflow.CaptureDecision{name: {Capture: true, Resolved: true, Selected: true}})); err != nil {
		t.Fatal(err)
	}
	if err := profile.Validate(*data); err != nil {
		t.Fatal(err)
	}
	unit := data.Services.Units[0]
	if unit.Presence != profile.ServiceAbsent || len(unit.Instances) != 1 || unit.Instances[0].Presence != profile.ServiceAbsent || unit.Instances[0].StartIntent != profile.ServiceStartNotManaged {
		t.Fatalf("reviewed Capture did not retain template/instance absence: %+v", unit)
	}
	// Reconstruct the destination's pre-Restore state independently of the
	// captured machine: its template and persistent enabled instance remain.
	if err := os.WriteFile(live, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	instance := observedService("backup@photos.service", live)
	instance.InstanceOf, instance.StartIntent = name, profile.ServiceStartEnabled
	s.units = []ObservedUnit{template, instance}
	return p, data, s, live
}

func TestPlanExactAbsentTemplateStillChecksInstanceTombstone(t *testing.T) {
	for _, state := range []string{"enabled", "disabled", "not-listed"} {
		t.Run(state, func(t *testing.T) {
			p, data, s, live := capturedAbsentTemplateFixture(t)
			if state == "disabled" {
				s.units[1].StartIntent = profile.ServiceStartDisabled
			} else if state == "not-listed" {
				s.units = s.units[:1]
			}
			fragment := persistentPlan(t, p, *data, false, true)
			if state == "enabled" {
				if len(fragment.Operations) != 0 || fragment.Compatibility.Authority != model.CompatibilityReduced || len(fragment.Compatibility.Findings) != 1 || fragment.Compatibility.Findings[0].Code != "services.exact.instance" {
					t.Fatalf("absent template ignored unproved instance removal: %+v", fragment)
				}
			} else if len(fragment.Operations) != 2 || fragment.Operations[0].Delete == nil || fragment.Operations[0].Delete.Destination != live || fragment.Compatibility.State != model.CompatibilitySupported {
				t.Fatalf("satisfied instance tombstone suppressed guarded template removal: %+v", fragment)
			}
		})
	}
}

func TestVerifyAbsentTemplateDoesNotIgnoreConfiguredInstance(t *testing.T) {
	p, data, s, live := capturedAbsentTemplateFixture(t)
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units = s.units[1:]
	result, err := p.Verify(context.Background(), *data, planContext(*data, false, true))
	if err != nil || result.OK || len(result.Missing) != 1 || result.Missing[0] != "backup@.service" {
		t.Fatalf("enabled instance falsely converged after template removal: %+v err=%v", result, err)
	}
	s.units[0].StartIntent = profile.ServiceStartDisabled
	result, err = p.Verify(context.Background(), *data, planContext(*data, false, true))
	if err != nil || !result.OK {
		t.Fatalf("disabled instance tombstone failed convergence: %+v err=%v", result, err)
	}
	s.units = nil
	result, err = p.Verify(context.Background(), *data, planContext(*data, false, true))
	if err != nil || !result.OK {
		t.Fatalf("fully absent template/instance required removal authority: %+v err=%v", result, err)
	}
}

func TestExactAbsentTemplateStillProcessesInstanceArtifacts(t *testing.T) {
	for _, kind := range []string{"drop-in", "mask"} {
		t.Run(kind, func(t *testing.T) {
			p, data, _, path := absentInstanceDropInFixture(t)
			unit := &data.Services.Units[0]
			unit.Presence, unit.StartIntent = profile.ServiceAbsent, profile.ServiceStartNotManaged
			live := filepath.Join(p.Roots.UserConfigDir, unit.Name)
			if err := os.Remove(live); err != nil {
				t.Fatal(err)
			}
			if kind == "mask" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				instance := &unit.Instances[0]
				instance.Mask = &profile.ServiceMask{Presence: profile.ServiceAbsent}
				path = filepath.Join(p.Roots.UserConfigDir, instance.Name)
				if err := os.Symlink("/dev/null", path); err != nil {
					t.Fatal(err)
				}
			}
			verified, err := p.Verify(context.Background(), *data, planContext(*data, false, true))
			if err != nil || verified.OK {
				t.Fatalf("absent template ignored remaining instance %s: %+v err=%v", kind, verified, err)
			}
			fragment := persistentPlan(t, p, *data, false, true)
			if kind == "drop-in" {
				if len(fragment.Operations) != 2 || fragment.Operations[0].Delete == nil || fragment.Operations[0].Delete.Destination != path || fragment.Operations[0].Delete.ExpectedExisting == nil || !hasCommand(fragment, "daemon-reload") {
					t.Fatalf("absent template skipped guarded instance drop-in cleanup: %+v", fragment)
				}
				journal, err := restore.NewJournal(t.TempDir(), time.Now())
				if err != nil {
					t.Fatal(err)
				}
				defer journal.Close()
				result, err := restore.Execute(context.Background(), &recordingRunner{}, model.RestorePlan{Operations: fragment.Operations}, journal, time.Now, time.Second, nil)
				if err != nil || len(result.Failed) != 0 || len(result.Blocked) != 0 {
					t.Fatalf("instance cleanup under absent template failed: %+v err=%v", result, err)
				}
			} else {
				if len(fragment.Operations) != 0 || fragment.Compatibility.Authority != model.CompatibilityReduced || len(fragment.Compatibility.Findings) != 1 || fragment.Compatibility.Findings[0].Code != "services.exact.mask" {
					t.Fatalf("absent template ignored unproved instance mask removal: %+v", fragment)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			verified, err = p.Verify(context.Background(), *data, planContext(*data, false, true))
			if err != nil || !verified.OK {
				t.Fatalf("cleared instance %s did not converge under absent template: %+v err=%v", kind, verified, err)
			}
		})
	}
}

func TestAbsentTemplateCannotGrantPositiveInstanceStateAuthority(t *testing.T) {
	p, data, s, live := capturedAbsentTemplateFixture(t)
	data.Services.Units[0].Instances[0].Presence = profile.ServicePresent
	data.Services.Units[0].Instances[0].StartIntent = profile.ServiceStartEnabled
	if err := profile.Validate(*data); err != nil {
		t.Fatal(err)
	}
	s.units[1].StartIntent = profile.ServiceStartDisabled
	fragment := persistentPlan(t, p, *data, false, true)
	if len(fragment.Operations) != 0 || fragment.Compatibility.Authority != model.CompatibilityBlocked {
		t.Fatalf("positive instance state received authority under absent template: %+v", fragment)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units[1].StartIntent = profile.ServiceStartEnabled
	verified, err := p.Verify(context.Background(), *data, planContext(*data, false, true))
	if err != nil || verified.OK {
		t.Fatalf("positive instance state converged without a desired-present template: %+v err=%v", verified, err)
	}
}

func TestAbsentTemplateCannotValidatePresentInstanceDropIn(t *testing.T) {
	p, data, _, path := absentInstanceDropInFixture(t)
	unit := &data.Services.Units[0]
	unit.Presence, unit.StartIntent = profile.ServiceAbsent, profile.ServiceStartNotManaged
	artifact := &unit.Instances[0].DropIns[0]
	artifact.Presence = profile.ServicePresent
	snapshot := filepath.Join(p.ProfileDir, "services", filepath.FromSlash(artifact.Path))
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(snapshot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(p.Roots.UserConfigDir, unit.Name)); err != nil {
		t.Fatal(err)
	}
	fragment := persistentPlan(t, p, *data, false, true)
	if len(fragment.Operations) != 0 || fragment.Compatibility.Authority != model.CompatibilityBlocked || len(fragment.Compatibility.Findings) != 1 || fragment.Compatibility.Findings[0].Code != "services.base.unavailable" {
		t.Fatalf("present instance drop-in lacked a validated template base: %+v", fragment)
	}
	verified, err := p.Verify(context.Background(), *data, planContext(*data, false, true))
	if err != nil || verified.OK {
		t.Fatalf("present instance drop-in converged without a template base: %+v err=%v", verified, err)
	}
}
