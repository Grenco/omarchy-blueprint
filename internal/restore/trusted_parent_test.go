package restore

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

type fakeDirInfo struct {
	os.FileInfo
	mode os.FileMode
	uid  uint32
}

func (f fakeDirInfo) Mode() os.FileMode { return f.mode | os.ModeDir }
func (f fakeDirInfo) IsDir() bool       { return true }
func (f fakeDirInfo) Sys() any          { return &syscall.Stat_t{Uid: f.uid} }

// SEC-02: a parent another user could swap for a symlink is refused.
func TestTrustedParentRefusesDirectoriesOtherUsersCanChange(t *testing.T) {
	const me = 1000
	for _, tc := range []struct {
		name string
		info fakeDirInfo
		ok   bool
	}{
		{"own directory", fakeDirInfo{mode: 0o755, uid: me}, true},
		{"root-owned system directory", fakeDirInfo{mode: 0o755, uid: 0}, true},
		{"sticky world-writable /tmp", fakeDirInfo{mode: 0o777 | os.ModeSticky, uid: 0}, true},
		{"group-writable (user private group)", fakeDirInfo{mode: 0o775, uid: me}, true},
		{"owned by another user", fakeDirInfo{mode: 0o755, uid: 2000}, false},
		{"another user's directory under /tmp", fakeDirInfo{mode: 0o700, uid: 2000}, false},
		{"world-writable without sticky", fakeDirInfo{mode: 0o777, uid: me}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := trustedParent("/x", tc.info, me); (err == nil) != tc.ok {
				t.Fatalf("trustedParent = %v, want ok=%t", err, tc.ok)
			}
		})
	}
}

func TestRestoreRefusesToWriteBeneathAWorldWritableDirectory(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	data := []byte("restored")
	destination := filepath.Join(shared, "app", "config")
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "w", Provider: "config", Action: "write", Resource: "config", File: &model.FileWrite{Generated: true, Content: data, Destination: destination, SourceHash: generatedHash(data), ExpectedMissing: true, RejectSymlinkParents: true}}}}
	result, err := Execute(t.Context(), nil, plan, journal, time.Now, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failed) != 1 || !strings.Contains(result.Failed[0].Error, "writable by every user") {
		t.Fatalf("write beneath a world-writable directory was not refused: %+v", result)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("the file was written")
	}
}
