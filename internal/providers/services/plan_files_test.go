package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanArtifactCreateAndSafeForceConflict(t *testing.T) {
	p, _, _, live := captureFixture(t)
	file, err := readServiceFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	op, reason, err := p.planArtifact("backup.service", "units/backup.service", live, file, false, false)
	if err != nil || reason != "" || op.File == nil || !op.File.ExpectedMissing || !op.File.RejectSymlinkParents {
		t.Fatalf("missing artifact: op=%+v reason=%s err=%v", op, reason, err)
	}
	if err := os.WriteFile(live, []byte("destination customization"), 0o644); err != nil {
		t.Fatal(err)
	}
	op, reason, err = p.planArtifact("backup.service", "units/backup.service", live, file, false, false)
	if err != nil || reason == "" || op.File != nil {
		t.Fatalf("Safe overwrote conflict: op=%+v reason=%s err=%v", op, reason, err)
	}
	op, reason, err = p.planArtifact("backup.service", "units/backup.service", live, file, true, false)
	if err != nil || reason != "" || op.File == nil || !op.File.Backup || !op.File.ReplaceExisting || op.File.ExpectedExisting == nil {
		t.Fatalf("Force lacked guarded replacement: op=%+v reason=%s err=%v", op, reason, err)
	}
}

func TestPlanArtifactExactRequiresMatchingPriorProvenance(t *testing.T) {
	p, _, _, live := captureFixture(t)
	file, err := readServiceFile(live)
	if err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"", "different"} {
		prior := capturedFile{hash: hash, mode: file.mode}
		op, reason, err := p.planArtifact("backup.service", "units/backup.service", live, prior, true, true)
		if err != nil || op.Delete != nil || reason == "" {
			t.Fatalf("Exact gained authority from incomplete provenance: op=%+v reason=%s err=%v", op, reason, err)
		}
	}
	op, reason, err := p.planArtifact("backup.service", "units/backup.service", live, file, false, true)
	if err != nil || reason != "" || op.Delete == nil || op.Delete.ExpectedExisting == nil || !op.Delete.Backup {
		t.Fatalf("matching Exact removal unguarded: op=%+v reason=%s err=%v", op, reason, err)
	}
}

func TestPlanArtifactCannotReplaceExternalOrLinkedTarget(t *testing.T) {
	p, _, _, live := captureFixture(t)
	file, err := readServiceFile(live)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external.service")
	if err := os.WriteFile(external, []byte("external definition"), 0o644); err != nil {
		t.Fatal(err)
	}
	op, reason, err := p.planArtifact("backup.service", "units/backup.service", external, file, true, false)
	if err != nil || reason == "" || op.File != nil {
		t.Fatalf("external target acquired: op=%+v reason=%s err=%v", op, reason, err)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, live); err != nil {
		t.Fatal(err)
	}
	op, reason, err = p.planArtifact("backup.service", "units/backup.service", live, file, true, false)
	if err != nil || reason == "" || op.File != nil {
		t.Fatalf("linked target replaced: op=%+v reason=%s err=%v", op, reason, err)
	}
}
