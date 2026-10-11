package workflow

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"os"
	"reflect"
	"testing"
)

func syncTestView(t *testing.T, s *Session, d profile.Data) SyncProfileView {
	t.Helper()
	root := t.TempDir()
	if err := profile.Save(root, d); err != nil {
		t.Fatal(err)
	}
	targets, err := projectSyncProfile(d, s.providers)
	if err != nil {
		t.Fatal(err)
	}
	control, err := syncControl(d, s.machine.Name)
	if err != nil {
		t.Fatal(err)
	}
	return SyncProfileView{Root: root, Profile: d, Targets: targets, Control: control}
}
func TestSyncComposeDisjointAndStopManaging(t *testing.T) {
	s, d, _ := syncViewRepo(t)
	d.Packages.Official = []string{"shared"}
	base := syncTestView(t, s, d)
	left := profile.CloneData(d)
	left.Packages.Official = []string{"shared", "local"}
	right := profile.CloneData(d)
	right.Packages.Official = []string{"shared", "remote"}
	local, remote := syncTestView(t, s, left), syncTestView(t, s, right)
	root, cleanup, err := s.composeSyncProfileTree(context.Background(), base, local, remote, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := profile.Load(root)
	if err != nil || !reflect.DeepEqual(got.Packages.Official, []string{"local", "remote", "shared"}) {
		t.Fatal(got.Packages, err)
	}
	cleanup()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("composition leaked")
	}
	left.Packages.Official = nil
	local = syncTestView(t, s, left)
	root, cleanup, err = s.composeSyncProfileTree(context.Background(), base, local, remote, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	got, err = profile.Load(root)
	if err != nil || !reflect.DeepEqual(got.Packages.Official, []string{"remote"}) || len(got.Packages.Absent) != 0 {
		t.Fatal("unmanaged became tombstone", got.Packages, err)
	}
}

func TestSyncComposeConflictChoicesAndControl(t *testing.T) {
	s, d, _ := syncViewRepo(t)
	d.Packages.Official = nil
	d.Packages.Mise = profile.MiseTools{"go": {"version": "base"}}
	left, right := profile.CloneData(d), profile.CloneData(d)
	left.Packages.Mise["go"]["version"] = "local"
	right.Packages.Mise["go"]["version"] = "remote"
	key := syncengine.TargetKey{Category: "packages", Target: "mise:go"}
	base, local, remote := syncTestView(t, s, d), syncTestView(t, s, left), syncTestView(t, s, right)
	for _, resolution := range []syncengine.Resolution{"", syncengine.ResolutionDefer} {
		if _, cleanup, err := s.composeSyncProfileTree(context.Background(), base, local, remote, map[syncengine.TargetKey]syncengine.Resolution{key: resolution}); err == nil {
			if cleanup != nil {
				cleanup()
			}
			t.Fatal("unresolved conflict composed")
		}
	}
	for _, choice := range []syncengine.Resolution{syncengine.ResolutionKeepLocal, syncengine.ResolutionAcceptRemote} {
		root, cleanup, err := s.composeSyncProfileTree(context.Background(), base, local, remote, map[syncengine.TargetKey]syncengine.Resolution{key: choice})
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := profile.Load(root)
		cleanup()
		want := "remote"
		if choice == syncengine.ResolutionKeepLocal {
			want = "local"
		}
		if err != nil || loaded.Packages.Mise["go"]["version"] != want {
			t.Fatal(loaded.Packages.Mise, err)
		}
	}
	// Authority disagreement is separate: a target choice cannot resolve it.
	left.Policy.Capture = []policy.Rule{{Category: "packages", Setting: policy.SettingDisabled}}
	right.Policy.Restore = []policy.Rule{{Category: "packages", Setting: policy.SettingDisabled}}
	local, remote = syncTestView(t, s, left), syncTestView(t, s, right)
	if _, cleanup, err := s.composeSyncProfileTree(context.Background(), base, local, remote, map[syncengine.TargetKey]syncengine.Resolution{key: syncengine.ResolutionKeepLocal}); err == nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("two-sided control disagreement composed")
	}
	// One-sided control follows its source, identical two-sided converges.
	right.Policy = left.Policy
	local, remote = syncTestView(t, s, left), syncTestView(t, s, right)
	root, cleanup, err := s.composeSyncProfileTree(context.Background(), base, local, remote, map[syncengine.TargetKey]syncengine.Resolution{key: syncengine.ResolutionKeepLocal})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _ := profile.Load(root)
	cleanup()
	if !reflect.DeepEqual(loaded.Policy, left.Policy) {
		t.Fatal("control lost")
	}
	right.Policy = policy.Rules{}
	remote = syncTestView(t, s, right)
	root, cleanup, err = s.composeSyncProfileTree(context.Background(), base, local, remote, map[syncengine.TargetKey]syncengine.Resolution{key: syncengine.ResolutionKeepLocal})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _ = profile.Load(root)
	cleanup()
	if !reflect.DeepEqual(loaded.Policy, left.Policy) {
		t.Fatal("one-sided local control lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.composeSyncProfileTree(ctx, base, local, remote, nil); err != context.Canceled {
		t.Fatal(err)
	}
}
