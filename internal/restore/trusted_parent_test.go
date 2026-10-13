package restore

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
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
	gid  uint32
}

func (f fakeDirInfo) Mode() os.FileMode { return f.mode | os.ModeDir }
func (f fakeDirInfo) IsDir() bool       { return true }
func (f fakeDirInfo) Sys() any          { return &syscall.Stat_t{Uid: f.uid, Gid: f.gid} }

const (
	me           = 1000
	privateGroup = 1000 // holds only me
	sharedGroup  = 100  // holds me and another user
	rootGroup    = 0    // holds only root
	unknownGroup = 4242 // not a local group
)

func fakeGroups(t *testing.T) {
	t.Helper()
	previous := groupMembers
	groupMembers = func(gid uint32) ([]uint32, error) {
		switch gid {
		case privateGroup:
			return []uint32{me}, nil
		case sharedGroup:
			return []uint32{me, 2000}, nil
		case rootGroup:
			return []uint32{0}, nil
		}
		return nil, errors.New("group is not a local group")
	}
	t.Cleanup(func() { groupMembers = previous })
}

// SEC-02: a parent another user could swap for a symlink is refused.
func TestTrustedParentRefusesDirectoriesOtherUsersCanChange(t *testing.T) {
	fakeGroups(t)
	for _, tc := range []struct {
		name   string
		info   fakeDirInfo
		ok     bool
		shared bool
	}{
		{"own directory", fakeDirInfo{mode: 0o755, uid: me, gid: privateGroup}, true, false},
		{"root-owned system directory", fakeDirInfo{mode: 0o755, uid: 0, gid: rootGroup}, true, false},
		{"sticky world-writable /tmp", fakeDirInfo{mode: 0o777 | os.ModeSticky, uid: 0, gid: rootGroup}, true, true},
		{"group-writable, user private group", fakeDirInfo{mode: 0o775, uid: me, gid: privateGroup}, true, false},
		{"group-writable, root's group", fakeDirInfo{mode: 0o775, uid: 0, gid: rootGroup}, true, false},
		{"own directory, group-writable by a shared group", fakeDirInfo{mode: 0o775, uid: me, gid: sharedGroup}, false, false},
		{"root-owned directory, group-writable by a shared group", fakeDirInfo{mode: 0o775, uid: 0, gid: sharedGroup}, false, false},
		{"group-writable by a group that cannot be confirmed", fakeDirInfo{mode: 0o775, uid: me, gid: unknownGroup}, false, false},
		{"sticky, group-writable by a shared group", fakeDirInfo{mode: 0o775 | os.ModeSticky, uid: 0, gid: sharedGroup}, true, true},
		{"shared group without write access", fakeDirInfo{mode: 0o755, uid: me, gid: sharedGroup}, true, false},
		{"owned by another user", fakeDirInfo{mode: 0o755, uid: 2000, gid: privateGroup}, false, false},
		{"another user's directory under /tmp", fakeDirInfo{mode: 0o700, uid: 2000, gid: privateGroup}, false, false},
		{"world-writable without sticky", fakeDirInfo{mode: 0o777, uid: me, gid: privateGroup}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shared, err := trustedParent("/x", tc.info, me)
			if (err == nil) != tc.ok || shared != tc.shared {
				t.Fatalf("trustedParent = shared %t, %v; want ok=%t shared=%t", shared, err, tc.ok, tc.shared)
			}
		})
	}
}

// In a sticky shared directory, another user cannot rename this user's
// entries but can create new ones: a missing destination parent there
// could be created first, as a symlink, and is refused.
func TestMissingParentInASharedStickyDirectoryIsRefused(t *testing.T) {
	sticky := filepath.Join(t.TempDir(), "sticky")
	if err := os.Mkdir(sticky, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sticky, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := validateSymlinkParents(filepath.Join(sticky, "app", "config")); err == nil || !strings.Contains(err.Error(), "other users can create it") {
		t.Fatalf("missing parent in a sticky shared directory: %v", err)
	}
	if err := os.Mkdir(filepath.Join(sticky, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateSymlinkParents(filepath.Join(sticky, "app", "config")); err != nil {
		t.Fatalf("an existing own directory in a sticky directory was refused: %v", err)
	}
	if err := validateSymlinkParents(filepath.Join(sticky, "app", "more", "config")); err != nil {
		t.Fatalf("a missing directory under an own one was refused: %v", err)
	}
}

func TestRestoreRefusesToWriteBeneathASharedGroupDirectory(t *testing.T) {
	fakeGroups(t)
	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(shared, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o775); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(shared)
	if err != nil {
		t.Fatal(err)
	}
	previous := groupMembers
	groupMembers = func(gid uint32) ([]uint32, error) {
		if gid == info.Sys().(*syscall.Stat_t).Gid {
			return []uint32{uint32(os.Geteuid()), 2000}, nil
		}
		return previous(gid)
	}
	destination := filepath.Join(shared, "app", "config")
	if err := validateSymlinkParents(destination); err == nil || !strings.Contains(err.Error(), "has other members") {
		t.Fatalf("write beneath a shared-group directory was not refused: %v", err)
	}
}

// The real lookup agrees with this machine's own private group, if it has
// one in /etc/group.
func TestSystemGroupMembersReadsLocalGroups(t *testing.T) {
	members, err := systemGroupMembers(0)
	if err != nil {
		t.Skipf("no local root group: %v", err)
	}
	if !slices.Contains(members, 0) {
		t.Fatalf("root group members = %v, want root among them", members)
	}
	if _, err := systemGroupMembers(4294967294); err == nil {
		t.Fatal("an undefined group was confirmed")
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
