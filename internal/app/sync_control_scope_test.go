package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/gittest"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestSyncConfigScopeChangeCannotHideMachineCustomization(t *testing.T) {
	for _, rule := range []string{"include", "exclude"} {
		t.Run(rule, func(t *testing.T) {
			root, deps := configSandbox(t)
			_, user, _ := deps.ConfigDirs()
			home := filepath.Dir(user)
			deps.HomeDir = func() (string, error) { return home, nil }
			deps.PluginDir = func() (string, error) { return filepath.Join(home, "plugins"), nil }
			deps.MiseGlobalConfig = func() (string, error) { return filepath.Join(home, "mise.toml"), nil }
			local := profile.New("scope-fixture", time.Time{})
			local.Manifest.Capture.Config = true
			local.Machines.Items = []profile.Machine{{Name: "test-machine"}}
			remote := profile.CloneData(local)
			var livePath string
			if rule == "include" {
				remote.Config.Included = []string{".config/writer/themes"}
				livePath = ".config/writer/themes/dark.css"
				if err := os.MkdirAll(filepath.Join(user, "writer/Cache"), 0755); err != nil {
					t.Fatal(err)
				}
			} else {
				local.Config.Excluded = []string{".config/hypr"}
				livePath = ".config/hypr/bindings.lua"
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(home, livePath)), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, livePath), []byte("custom fixture"), 0644); err != nil {
				t.Fatal(err)
			}
			// Establish the scope discrepancy using the real Config scan, not a
			// fake target projection. Only the selected remote rules see the file.
			p := configStateProvider{deps: deps, opt: &options{profileDir: root}}
			for _, scope := range []struct {
				data    profile.Data
				visible bool
			}{{local, false}, {remote, true}} {
				detector, err := p.provider(scope.data)
				if err != nil {
					t.Fatal(err)
				}
				scan, err := detector.Scan(scope.data.Config)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, c := range scan.Candidates {
					found = found || c.Path == livePath
				}
				if found != scope.visible {
					t.Fatal("fixture does not distinguish rule scopes", rule, scope.visible, scan)
				}
			}
			if err := profile.Save(root, local); err != nil {
				t.Fatal(err)
			}
			gitRun(t, "-C", root, "init", "-b", "main")
			gitRun(t, "-C", root, "config", "user.email", "test@example.invalid")
			gitRun(t, "-C", root, "config", "user.name", "Test")
			gitRun(t, "-C", root, "add", ".")
			gitRun(t, "-C", root, "commit", "-m", "base")
			origin := filepath.Join(t.TempDir(), "remote.git")
			gitRun(t, "init", "--bare", origin)
			gittest.ConfigureRemote(t, origin)
			gitRun(t, "-C", root, "remote", "add", "origin", origin)
			gitRun(t, "-C", root, "push", "-u", "origin", "main")
			info, err := (profilegit.Service{Root: root, Runner: command.SystemRunner{}}).CurrentRevisionInfo(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			state, _ := deps.StateHome()
			store := syncengine.CursorStore{StateHome: state, ProfileRoot: root}
			cursor := syncengine.Cursor{Version: syncengine.CursorFormatVersion, Revision: info.SHA, Machine: "test-machine", Branch: info.Branch, Upstream: info.Upstream, RemoteFingerprint: info.OriginFingerprint}
			if err := store.Save(cursor); err != nil {
				t.Fatal(err)
			}
			peer := filepath.Join(t.TempDir(), "peer")
			gitRun(t, "clone", "-b", "main", origin, peer)
			gitRun(t, "-C", peer, "config", "user.email", "test@example.invalid")
			gitRun(t, "-C", peer, "config", "user.name", "Test")
			if err := profile.Save(peer, remote); err != nil {
				t.Fatal(err)
			}
			gitRun(t, "-C", peer, "add", ".")
			gitRun(t, "-C", peer, "commit", "-m", "remote scope")
			gitRun(t, "-C", peer, "push")
			deps.Runner = &syncCLIRunner{machine: deps.Runner.(*machineRunner)}
			s, err := openWorkflow(deps, &options{profileDir: root, machine: "test-machine"})
			if err != nil {
				t.Fatal(err)
			}
			index := gitRun(t, "-C", root, "write-tree")
			plan, err := s.InspectSync(context.Background(), workflow.SyncInspectOptions{Fetch: true})
			if err != nil {
				t.Fatal(err)
			}
			blocked := false
			for _, b := range plan.Blockers {
				blocked = blocked || b.Code == "control-reinspection"
			}
			if !blocked || len(plan.Items) != 0 {
				t.Fatal("unobserved customization accepted under changed control", rule, plan)
			}
			if gitRun(t, "-C", root, "write-tree") != index || strings.TrimSpace(gitRun(t, "-C", root, "rev-parse", "HEAD")) != info.SHA || gitRun(t, "-C", root, "status", "--porcelain") != "" {
				t.Fatal("inspection mutated active checkout")
			}
			after, _, err := store.Load()
			if err != nil || after != cursor {
				t.Fatal("inspection changed cursor", err)
			}
		})
	}
}
