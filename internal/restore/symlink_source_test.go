package restore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func TestSymlinkSourcePreconditionRejectsChangedOrMissingFile(t *testing.T) {
	for _, missing := range []bool{false, true} {
		source := filepath.Join(t.TempDir(), "source.service")
		if err := os.WriteFile(source, []byte("captured source"), 0o644); err != nil {
			t.Fatal(err)
		}
		precondition := filesystemPreconditionForTest(t, source)
		destination := filepath.Join(t.TempDir(), "unit.service")
		action := model.SymlinkWrite{Target: source, Destination: destination, ExpectedMissing: true, ExpectedTarget: &precondition, RejectSymlinkParents: true}
		if missing {
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(source, []byte("changed source"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := executeSymlinkWrite(action); err == nil {
			t.Fatal("unapproved source created a link")
		}
		if _, err := os.Lstat(destination); !os.IsNotExist(err) {
			t.Fatalf("rejected source still created destination: %v", err)
		}
	}
}
func TestSymlinkSourcePreconditionAcceptsMatchingFile(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.service")
	if err := os.WriteFile(source, []byte("matching source"), 0o644); err != nil {
		t.Fatal(err)
	}
	precondition := filesystemPreconditionForTest(t, source)
	destination := filepath.Join(t.TempDir(), "unit.service")
	if err := executeSymlinkWrite(model.SymlinkWrite{Target: source, Destination: destination, ExpectedMissing: true, ExpectedTarget: &precondition, RejectSymlinkParents: true}); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(destination); err != nil || target != source {
		t.Fatalf("wrong link: target=%s err=%v", target, err)
	}
}
