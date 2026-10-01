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
)

func absentInstanceDropInFixture(t *testing.T) (*Provider, *profile.Data, *planSystemd, string) {
	t.Helper()
	p, data, s, _ := persistentTemplateFixture(t)
	name := "backup@photos.service"
	path := filepath.Join(p.Roots.UserConfigDir, name+".d", "10-custom.conf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[Service]\nEnvironment=MODE=managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := readServiceFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data.Services.Units[0].Instances = []profile.ServiceInstance{
		{Name: name, Presence: profile.ServiceAbsent, StartIntent: profile.ServiceStartNotManaged,
			DropIns: []profile.ServiceArtifact{{Path: "units/" + name + ".d/10-custom.conf", Presence: profile.ServiceAbsent, Hash: file.hash, Mode: file.mode}}},
		// This unrelated satisfied tombstone sorts before the instance with
		// managed artifact work and must not suppress that work.
		{Name: "backup@archive.service", Presence: profile.ServiceAbsent, StartIntent: profile.ServiceStartNotManaged},
	}
	return p, data, s, path
}

func TestPlanExactAbsentInstanceStillRemovesManagedDropInTombstone(t *testing.T) {
	for _, state := range []string{"not-listed", "disabled"} {
		t.Run(state, func(t *testing.T) {
			p, data, s, path := absentInstanceDropInFixture(t)
			if state == "disabled" {
				actual := observedService("backup@photos.service", s.units[0].FragmentPath)
				actual.InstanceOf, actual.DropInPaths = "backup@.service", []string{path}
				s.units = append(s.units, actual)
			}
			unmanaged := filepath.Join(filepath.Dir(path), "99-unmanaged.conf")
			if err := os.WriteFile(unmanaged, []byte("[Service]\nEnvironment=KEEP=yes\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			additive := persistentPlan(t, p, *data, false, false)
			if len(additive.Operations) != 0 {
				t.Fatalf("Additive acquired instance tombstone authority: %+v", additive)
			}
			fragment := persistentPlan(t, p, *data, false, true)
			if fragment.Compatibility.State != model.CompatibilitySupported || fragment.Compatibility.Authority != model.CompatibilityUnchanged || len(fragment.Operations) != 2 {
				t.Fatalf("absent instance suppressed managed drop-in deletion: %+v", fragment)
			}
			deletion := fragment.Operations[0]
			if deletion.Delete == nil || deletion.Delete.Destination != path || deletion.Delete.ExpectedExisting == nil || !deletion.Delete.Backup || !deletion.Delete.RejectSymlinkParents || !hasCommand(fragment, "daemon-reload") {
				t.Fatalf("instance drop-in removal lost execution safeguards: %+v", fragment)
			}
			if len(fragment.Operations[1].DependsOn) != 1 || fragment.Operations[1].DependsOn[0] != deletion.ID {
				t.Fatalf("refresh did not depend on drop-in deletion: %+v", fragment)
			}
			journal, err := restore.NewJournal(t.TempDir(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			result, err := restore.Execute(context.Background(), &recordingRunner{}, model.RestorePlan{Operations: fragment.Operations}, journal, time.Now, time.Second, nil)
			if err != nil || len(result.Failed) != 0 || len(result.Blocked) != 0 {
				t.Fatalf("guarded drop-in deletion failed: %+v err=%v", result, err)
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("Exact did not remove the selected drop-in: %v", err)
			}
			if _, err := os.Stat(unmanaged); err != nil {
				t.Fatalf("Exact removed an unrelated file: %v", err)
			}
			verified, err := p.Verify(context.Background(), *data, planContext(*data, false, true))
			if err != nil || !verified.OK {
				t.Fatalf("removed drop-in did not converge: %+v err=%v", verified, err)
			}
		})
	}
}

func TestPlanExactAbsentInstanceDropInRemovalRequiresProvenance(t *testing.T) {
	for _, proof := range []string{"missing-hash", "missing-mode", "changed-content", "changed-mode", "already-absent"} {
		t.Run(proof, func(t *testing.T) {
			p, data, _, path := absentInstanceDropInFixture(t)
			artifact := &data.Services.Units[0].Instances[0].DropIns[0]
			switch proof {
			case "missing-hash":
				artifact.Hash = ""
			case "missing-mode":
				artifact.Mode = ""
			case "changed-content":
				if err := os.WriteFile(path, []byte("[Service]\nEnvironment=MODE=unmanaged\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "changed-mode":
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
			case "already-absent":
				artifact.Hash, artifact.Mode = "", ""
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			fragment := persistentPlan(t, p, *data, true, true)
			if len(fragment.Operations) != 0 {
				t.Fatalf("Exact acquired unproved deletion authority: %+v", fragment)
			}
			if proof == "already-absent" {
				if fragment.Compatibility.State != model.CompatibilitySupported || fragment.Compatibility.Authority != model.CompatibilityUnchanged {
					t.Fatalf("already-absent drop-in required deletion proof: %+v", fragment)
				}
			} else if fragment.Compatibility.Authority != model.CompatibilityReduced || len(fragment.Skipped) == 0 {
				t.Fatalf("unproved drop-in removal was not visibly withheld: %+v", fragment)
			}
			verified, err := p.Verify(context.Background(), *data, planContext(*data, true, true))
			if err != nil || verified.OK != (proof == "already-absent") {
				t.Fatalf("Verify ignored remaining drop-in state: %+v err=%v", verified, err)
			}
		})
	}
}

func TestVerifyAbsentInstanceDoesNotIgnoreManagedDropIn(t *testing.T) {
	p, data, _, path := absentInstanceDropInFixture(t)
	result, err := p.Verify(context.Background(), *data, planContext(*data, false, true))
	if err != nil || result.OK || len(result.Missing) != 1 || result.Missing[0] != "backup@.service" {
		t.Fatalf("remaining instance drop-in falsely converged: %+v err=%v", result, err)
	}
	result, err = p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || !result.OK {
		t.Fatalf("Additive verified unapplied drop-in absence: %+v err=%v", result, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	result, err = p.Verify(context.Background(), *data, planContext(*data, false, true))
	if err != nil || !result.OK {
		t.Fatalf("already-absent drop-in failed Verify: %+v err=%v", result, err)
	}
}

func TestAbsentInstanceStillRestoresPresentManagedDropIn(t *testing.T) {
	p, data, s, path := absentInstanceDropInFixture(t)
	artifact := &data.Services.Units[0].Instances[0].DropIns[0]
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
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	verified, err := p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || verified.OK {
		t.Fatalf("missing present drop-in falsely converged: %+v err=%v", verified, err)
	}
	fragment := persistentPlan(t, p, *data, false, false)
	if len(fragment.Operations) != 2 || fragment.Operations[0].File == nil || fragment.Operations[0].File.Destination != path || !fragment.Operations[0].File.ExpectedMissing || !hasCommand(fragment, "daemon-reload") {
		t.Fatalf("absent instance suppressed independent present artifact intent: %+v", fragment)
	}
	if s.proposed["backup@photos.service.d/10-custom.conf"] != string(bytes) || s.proposed["backup@photos.service"] == "" {
		t.Fatalf("instance drop-in was not validated with its template base: %v", s.proposed)
	}
	journal, err := restore.NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	result, err := restore.Execute(context.Background(), &recordingRunner{}, model.RestorePlan{Operations: fragment.Operations}, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 0 || len(result.Blocked) != 0 {
		t.Fatalf("independent instance drop-in Restore failed: %+v err=%v", result, err)
	}
	verified, err = p.Verify(context.Background(), *data, planContext(*data, false, false))
	if err != nil || !verified.OK {
		t.Fatalf("restored present drop-in did not converge: %+v err=%v", verified, err)
	}
	if _, err := os.Lstat(filepath.Join(p.Roots.UserConfigDir, "backup@photos.service")); !os.IsNotExist(err) {
		t.Fatalf("instance artifact work duplicated the template definition: %v", err)
	}
}

func TestAbsentInstanceStillChecksManagedMaskTombstone(t *testing.T) {
	p, data, _, path := absentInstanceDropInFixture(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	instance := &data.Services.Units[0].Instances[0]
	instance.Mask = &profile.ServiceMask{Presence: profile.ServiceAbsent}
	maskPath := filepath.Join(p.Roots.UserConfigDir, instance.Name)
	if err := os.Symlink("/dev/null", maskPath); err != nil {
		t.Fatal(err)
	}
	fragment := persistentPlan(t, p, *data, true, true)
	if len(fragment.Operations) != 0 || fragment.Compatibility.Authority != model.CompatibilityReduced || len(fragment.Compatibility.Findings) != 1 || fragment.Compatibility.Findings[0].Code != "services.exact.mask" {
		t.Fatalf("absent instance ignored unproved mask removal: %+v", fragment)
	}
	result, err := p.Verify(context.Background(), *data, planContext(*data, true, true))
	if err != nil || result.OK {
		t.Fatalf("remaining instance mask falsely converged: %+v err=%v", result, err)
	}
	if err := os.Remove(maskPath); err != nil {
		t.Fatal(err)
	}
	result, err = p.Verify(context.Background(), *data, planContext(*data, true, true))
	if err != nil || !result.OK {
		t.Fatalf("already-absent mask failed Verify: %+v err=%v", result, err)
	}
}
