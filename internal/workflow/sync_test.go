package workflow

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/gittest"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func syncFixtureTreeHash(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%o\x00", rel, info.Mode())
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			h.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

type syncCountingRunner struct{ fetches atomic.Int32 }

func (r *syncCountingRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "git" {
		for _, a := range args {
			if a == "fetch" {
				r.fetches.Add(1)
			}
			switch a {
			case "pull", "merge", "reset", "checkout", "switch", "commit", "push":
				return "", os.ErrPermission
			}
		}
	}
	return (command.SystemRunner{}).Run(ctx, name, args...)
}

type syncLiveFake struct {
	syncPackageProvider
	live            profile.Data
	binds, observes *atomic.Int32
}

func (p *syncLiveFake) BindReadCycle(c *observation.Cycle) ReadProvider {
	p.binds.Add(1)
	return &syncLiveFakeRead{statusTestProvider: p.statusTestProvider, slot: observation.NewSlot(c, func(context.Context) (profile.Data, error) { p.observes.Add(1); return p.live, nil }, profile.CloneData)}
}

type syncLiveFakeRead struct {
	statusTestProvider
	slot *observation.Slot[profile.Data]
}

func (p *syncLiveFakeRead) SyncObserve(ctx context.Context, _ profile.Data, histories []profile.Data) (SyncLiveObservation, error) {
	d, err := p.slot.Get(ctx)
	if err != nil {
		return SyncLiveObservation{}, err
	}
	targets, err := (packages.Provider{}).SyncDesired(d)
	if err != nil {
		return SyncLiveObservation{}, err
	}
	inspections := []TargetInspection{}
	seen := map[syncengine.TargetKey]bool{}
	for _, x := range targets {
		seen[x.Key] = true
	}
	for _, historical := range histories {
		items, err := (packages.Provider{}).SyncDesired(historical)
		if err != nil {
			return SyncLiveObservation{}, err
		}
		for _, x := range items {
			if !seen[x.Key] {
				seen[x.Key] = true
				targets = append(targets, syncengine.DesiredTarget{Key: x.Key, State: syncengine.State{Kind: syncengine.StateUnmanaged}})
			}
		}
	}
	for _, x := range targets {
		inspections = append(inspections, TargetInspection{Key: x.Key.Target, Current: TargetPresent, CaptureEligible: true, RestoreEligible: true, Capabilities: TargetCapabilities{SupportsCapture: true, SupportsRestore: true, SupportsDesiredAbsence: true}})
	}
	return SyncLiveObservation{Targets: targets, Inspections: inspections}, nil
}

func syncTwoClones(t *testing.T) (*Session, *syncCountingRunner, profile.Data, string, string, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	s, d, base := syncViewRepo(t)
	root := s.ProfileDir()
	origin := filepath.Join(t.TempDir(), "origin.git")
	syncGit(t, root, "init", "--bare", origin)
	gittest.ConfigureRemote(t, origin)
	syncGit(t, root, "remote", "add", "origin", origin)
	syncGit(t, root, "push", "-u", "origin", "main")
	peer := filepath.Join(t.TempDir(), "peer")
	syncGit(t, root, "clone", "-b", "main", origin, peer)
	syncGit(t, peer, "config", "user.email", "test@example.invalid")
	syncGit(t, peer, "config", "user.name", "Test")
	runner := &syncCountingRunner{}
	s.deps.Runner = runner
	s.profileGit.Runner = runner
	binds, observes := &atomic.Int32{}, &atomic.Int32{}
	p := &syncLiveFake{syncPackageProvider: syncPackageProvider{statusTestProvider{id: "packages"}}, live: d, binds: binds, observes: observes}
	if err := s.SetProviders([]Provider{p}); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	s.deps.StateHome = func() (string, error) { return state, nil }
	info, err := s.profileGit.CurrentRevisionInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := (syncengine.CursorStore{StateHome: state, ProfileRoot: root}).Save(syncengine.Cursor{Version: 1, Machine: "test-machine", Revision: base, Branch: info.Branch, Upstream: info.Upstream, RemoteFingerprint: info.OriginFingerprint}); err != nil {
		t.Fatal(err)
	}
	return s, runner, d, base, peer, binds, observes
}

func TestSyncInspectTwoClonesReadOnlyOneCycle(t *testing.T) {
	s, runner, d, base, peer, binds, observes := syncTwoClones(t)
	root := s.ProfileDir()
	// Independent committed desired changes and same/different two-sided values.
	d.Packages.Mise = profile.MiseTools{"same": {"version": "base"}, "conflict": {"version": "base"}}
	// Move the actual shared baseline to include those common declarations.
	if err := profile.Save(root, d); err != nil {
		t.Fatal(err)
	}
	syncGit(t, root, "add", ".")
	syncGit(t, root, "commit", "-m", "common")
	syncGit(t, root, "push")
	syncGit(t, peer, "pull", "--ff-only")
	base = syncGit(t, root, "rev-parse", "HEAD")
	state, _ := s.deps.StateHome()
	store := syncengine.CursorStore{StateHome: state, ProfileRoot: root}
	cursor, _, _ := store.Load()
	cursor.Revision = base
	if err := store.Save(cursor); err != nil {
		t.Fatal(err)
	}
	left, right := profile.CloneData(d), profile.CloneData(d)
	left.Packages.Official = append(left.Packages.Official, "local")
	right.Packages.Official = append(right.Packages.Official, "remote")
	left.Packages.Mise["same"]["version"] = "new"
	right.Packages.Mise["same"]["version"] = "new"
	left.Packages.Mise["conflict"]["version"] = "left"
	right.Packages.Mise["conflict"]["version"] = "right"
	if err := profile.Save(root, left); err != nil {
		t.Fatal(err)
	}
	syncGit(t, root, "add", ".")
	syncGit(t, root, "commit", "-m", "local")
	if err := profile.Save(peer, right); err != nil {
		t.Fatal(err)
	}
	syncGit(t, peer, "add", ".")
	syncGit(t, peer, "commit", "-m", "remote")
	syncGit(t, peer, "push")
	local, remote := syncGit(t, root, "rev-parse", "HEAD"), syncGit(t, peer, "rev-parse", "HEAD")
	s.providers[0].(*syncLiveFake).live = left
	status, index := syncGit(t, root, "status", "--porcelain=v2"), syncGit(t, root, "write-tree")
	before, _ := os.ReadFile(filepath.Join(root, "packages", "official.txt"))
	treeBefore := syncFixtureTreeHash(t, root)
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Identity.Base != base || plan.Identity.Local != local || plan.Identity.Remote != remote {
		t.Fatal(plan.Identity)
	}
	classes := map[string]syncengine.HistoryClass{}
	for _, x := range plan.Items {
		classes[x.Key.Target] = x.History
	}
	for key, want := range map[string]syncengine.HistoryClass{"official:local": syncengine.HistoryLocal, "official:remote": syncengine.HistoryRemote, "mise:same": syncengine.HistoryConverged, "mise:conflict": syncengine.HistoryConflict} {
		if classes[key] != want {
			t.Fatal(key, classes)
		}
	}
	if runner.fetches.Load() != 1 || binds.Load() != 1 || observes.Load() != 1 {
		t.Fatal("duplicate fetch/cycle/observation", runner.fetches.Load(), binds.Load(), observes.Load())
	}
	after, _ := os.ReadFile(filepath.Join(root, "packages", "official.txt"))
	if syncFixtureTreeHash(t, root) != treeBefore {
		t.Fatal("profile tree changed")
	}
	if !reflect.DeepEqual(before, after) || syncGit(t, root, "status", "--porcelain=v2") != status || syncGit(t, root, "write-tree") != index || syncGit(t, root, "rev-parse", "HEAD") != local {
		t.Fatal("checkout/index changed")
	}
	saved, _, _ := store.Load()
	if saved != cursor {
		t.Fatal("inspection advanced cursor")
	}
	plan, err = s.InspectSync(context.Background(), SyncInspectOptions{})
	if err != nil || plan.Identity.Remote != remote || runner.fetches.Load() != 1 {
		t.Fatal("offline inspection fetched", err)
	}
}

func TestSyncInspectCursorMismatchBlocksWithoutRepair(t *testing.T) {
	for _, field := range []string{"machine", "branch", "upstream", "remote", "missing", "nonancestor"} {
		t.Run(field, func(t *testing.T) {
			s, runner, _, _, _, _, observes := syncTwoClones(t)
			state, _ := s.deps.StateHome()
			store := syncengine.CursorStore{StateHome: state, ProfileRoot: s.ProfileDir()}
			c, _, _ := store.Load()
			switch field {
			case "machine":
				c.Machine = "other"
			case "branch":
				c.Branch = "other"
			case "upstream":
				c.Upstream = "origin/other"
			case "remote":
				c.RemoteFingerprint = strings.Repeat("a", 64)
			case "missing":
				c.Revision = strings.Repeat("a", 40)
			case "nonancestor":
				syncGit(t, s.ProfileDir(), "commit", "--allow-empty", "-m", "future")
				c.Revision = syncGit(t, s.ProfileDir(), "rev-parse", "HEAD")
			}
			if err := store.Save(c); err != nil {
				t.Fatal(err)
			}
			plan, err := s.InspectSync(context.Background(), SyncInspectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Blockers) == 0 || len(plan.Items) != 0 || observes.Load() != 0 || runner.fetches.Load() != 0 {
				t.Fatal("invalid cursor accepted", plan)
			}
			after, _, _ := store.Load()
			if after != c {
				t.Fatal("cursor repaired implicitly")
			}
		})
	}
}

func TestSyncInspectionBlocksUnsafeRepositoryStates(t *testing.T) {
	for _, name := range []string{"detached", "no-upstream", "dirty", "control-conflict", "reverted-unmanaged-history", "invalid-history"} {
		t.Run(name, func(t *testing.T) {
			s, _, data, _, peer, _, _ := syncTwoClones(t)
			root := s.ProfileDir()
			commit := func(dir string) { syncGit(t, dir, "add", "."); syncGit(t, dir, "commit", "-m", "test change") }
			want := ""
			switch name {
			case "detached":
				syncGit(t, root, "checkout", "--detach")
				want = "git-identity"
			case "no-upstream":
				syncGit(t, root, "branch", "--unset-upstream")
				want = "git-identity"
			case "dirty":
				if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				want = "dirty-profile"
			case "control-conflict":
				left, right := profile.CloneData(data), profile.CloneData(data)
				left.Config.Included, right.Config.Included = []string{".config/left"}, []string{".config/right"}
				if err := profile.Save(root, left); err != nil {
					t.Fatal(err)
				}
				commit(root)
				if err := profile.Save(peer, right); err != nil {
					t.Fatal(err)
				}
				commit(peer)
				syncGit(t, peer, "push")
				want = "control-conflict"
			case "reverted-unmanaged-history":
				file := filepath.Join(root, "README.md")
				if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				commit(root)
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				commit(root)
				data.Packages.Official = append(data.Packages.Official, "remote")
				if err := profile.Save(peer, data); err != nil {
					t.Fatal(err)
				}
				commit(peer)
				syncGit(t, peer, "push")
				want = "non-managed-history"
			case "invalid-history":
				if err := os.WriteFile(filepath.Join(peer, "profile.toml"), []byte("invalid = ["), 0600); err != nil {
					t.Fatal(err)
				}
				commit(peer)
				syncGit(t, peer, "push")
				want = "historical-profile"
			}
			before, index, head := syncGit(t, root, "status", "--porcelain=v2"), syncGit(t, root, "write-tree"), syncGit(t, root, "rev-parse", "HEAD")
			plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, b := range plan.Blockers {
				found = found || b.Code == want
			}
			if !found {
				t.Fatal("missing blocker", want, plan.Blockers)
			}
			if syncGit(t, root, "status", "--porcelain=v2") != before || syncGit(t, root, "write-tree") != index || syncGit(t, root, "rev-parse", "HEAD") != head {
				t.Fatal("inspection mutated checkout")
			}
		})
	}
}
