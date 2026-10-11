package app

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/gittest"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyncInspectRemoteNonPortableDefaultBlocksRestore(t *testing.T) {
	root, deps := configSandbox(t)
	d := profile.New("test", time.Time{})
	d.Manifest.Capture.Defaults = true
	d.Defaults.Browser = "firefox"
	d.Machines.Items = []profile.Machine{{Name: "test-machine"}}
	if err := profile.Save(root, d); err != nil {
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
	if err := store.Save(syncengine.Cursor{Version: syncengine.CursorFormatVersion, Revision: info.SHA, Machine: "test-machine", Branch: info.Branch, Upstream: info.Upstream, RemoteFingerprint: info.OriginFingerprint}); err != nil {
		t.Fatal(err)
	}
	peer := filepath.Join(t.TempDir(), "peer")
	gitRun(t, "clone", "-b", "main", origin, peer)
	gitRun(t, "-C", peer, "config", "user.email", "test@example.invalid")
	gitRun(t, "-C", peer, "config", "user.name", "Test")
	d.Defaults.Browser = "custom.desktop"
	if err := profile.Save(peer, d); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", peer, "add", ".")
	gitRun(t, "-C", peer, "commit", "-m", "remote default")
	gitRun(t, "-C", peer, "push")
	runner := &syncCLIRunner{machine: deps.Runner.(*machineRunner)}
	runner.machine.defaults = map[string]string{"browser": "firefox"}
	runner.machine.allCommands = nil
	deps.Runner = runner
	home := t.TempDir()
	deps.HomeDir = func() (string, error) { return home, nil }
	deps.MiseGlobalConfig = func() (string, error) { return filepath.Join(home, "mise.toml"), nil }
	s, err := openWorkflow(deps, &options{profileDir: root, machine: "test-machine"})
	if err != nil {
		t.Fatal(err)
	}
	beforeIndex := gitRun(t, "-C", root, "write-tree")
	plan, err := s.InspectSync(context.Background(), workflow.SyncInspectOptions{Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range plan.Blockers {
		if b.Code == "restore-ineligible" && b.Target != nil && b.Target.Category == "defaults" && b.Target.Target == "browser" {
			found = true
		}
	}
	if !found {
		t.Fatal("unsafe remote default exposed without blocker", plan)
	}
	defaults, packages := 0, 0
	for _, call := range runner.machine.allCommands {
		if strings.Join(call, " ") == "omarchy default browser" {
			defaults++
		}
		if call[0] == "pacman-conf" {
			packages++
		}
	}
	if defaults != 1 || packages != 1 {
		t.Fatal("desired safety rescanned machine", defaults, packages)
	}
	if gitRun(t, "-C", root, "write-tree") != beforeIndex || strings.TrimSpace(gitRun(t, "-C", root, "rev-parse", "HEAD")) != info.SHA || gitRun(t, "-C", root, "status", "--porcelain") != "" {
		t.Fatal("inspection mutated checkout/index")
	}
	cursor, _, err := store.Load()
	if err != nil || cursor.Revision != info.SHA {
		t.Fatal("inspection advanced cursor", err)
	}
}

type syncCLIRunner struct {
	machine *machineRunner
	fetches int
}

func (r *syncCLIRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "git" {
		for _, a := range args {
			if a == "fetch" {
				r.fetches++
			}
			switch a {
			case "pull", "merge", "reset", "checkout", "switch", "commit", "push":
				return "", os.ErrPermission
			}
		}
		return (command.SystemRunner{}).Run(ctx, name, args...)
	}
	return r.machine.Run(ctx, name, args...)
}

func TestSyncCLIExplicitFetchOfflineAndJSON(t *testing.T) {
	root, deps := configSandbox(t)
	d := profile.New("test", time.Time{})
	d.Manifest.Capture.Packages = true
	d.Packages.Official = []string{"aether"}
	d.Machines.Items = []profile.Machine{{Name: "test-machine"}}
	if err := profile.Save(root, d); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", root, "init", "-b", "main")
	gitRun(t, "-C", root, "config", "user.email", "test@example.invalid")
	gitRun(t, "-C", root, "config", "user.name", "Test")
	gitRun(t, "-C", root, "add", ".")
	gitRun(t, "-C", root, "commit", "-m", "fixture")
	origin := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "init", "--bare", origin)
	gittest.ConfigureRemote(t, origin)
	gitRun(t, "-C", root, "remote", "add", "shared", origin)
	gitRun(t, "-C", root, "push", "-u", "shared", "main")
	sha := strings.TrimSpace(gitRun(t, "-C", root, "rev-parse", "HEAD"))
	before, index := gitRun(t, "-C", root, "status", "--porcelain=v2"), gitRun(t, "-C", root, "write-tree")
	runner := &syncCLIRunner{machine: deps.Runner.(*machineRunner)}
	runner.machine.preinstalls = map[string]bool{"aether": true}
	deps.Runner = runner
	home := t.TempDir()
	deps.HomeDir = func() (string, error) { return home, nil }
	mise := filepath.Join(t.TempDir(), "mise.toml")
	deps.MiseGlobalConfig = func() (string, error) { return mise, nil }
	for _, args := range [][]string{{"sync", "inspect", "--no-fetch"}, {"sync", "bootstrap", "inspect", "--no-fetch"}, {"sync", "inspect"}, {"sync", "bootstrap", "inspect"}} {
		out, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		deps.Out, deps.Err = out, stderr
		oldFetch := runner.fetches
		oldDetect := 0
		for _, call := range runner.machine.allCommands {
			if call[0] == "pacman-conf" {
				oldDetect++
			}
		}
		code := Execute(context.Background(), append([]string{"--profile", root, "--machine", "test-machine", "--json"}, args...), deps)
		if code != 0 {
			t.Fatal("inspection failed", args, stderr.String())
		}
		want := 1
		if args[len(args)-1] == "--no-fetch" {
			want = 0
		}
		if runner.fetches-oldFetch != want {
			t.Fatal("unexpected fetch count", args, runner.fetches-oldFetch)
		}
		detections := 0
		for _, call := range runner.machine.allCommands {
			if call[0] == "pacman-conf" {
				detections++
			}
		}
		wantDetect := 0
		if len(args) > 1 && args[1] == "bootstrap" {
			wantDetect = 1
		}
		if detections-oldDetect != wantDetect {
			t.Fatal("duplicate Packages observation", args, detections-oldDetect)
		}
		var envelope struct {
			Data struct {
				Sync struct {
					Identity syncengine.Identity `json:"identity"`
					Items    []struct {
						Key   syncengine.TargetKey `json:"key"`
						Local syncengine.State     `json:"local"`
					} `json:"items"`
				} `json:"sync"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err, out.String())
		}
		if envelope.Data.Sync.Identity.Local != sha || envelope.Data.Sync.Identity.Remote != sha {
			t.Fatal("lost exact revisions", out.String())
		}
		if wantDetect == 1 {
			found := false
			for _, x := range envelope.Data.Sync.Items {
				if x.Key.Target == "official:aether" {
					t.Fatal("legacy alias retained duplicate authority")
				}
				if x.Key.Target == "preinstall:aether" && x.Local.Kind == syncengine.StatePresent {
					found = true
				}
			}
			if !found {
				t.Fatal("historical alias not normalized through shared live context", out.String())
			}
		}
		if strings.Contains(out.String(), root) || strings.Contains(out.String(), origin) || strings.Contains(out.String(), "blueprint-sync-revision") {
			t.Fatal("private materialization/transport leaked")
		}
	}
	if gitRun(t, "-C", root, "status", "--porcelain=v2") != before || gitRun(t, "-C", root, "write-tree") != index || strings.TrimSpace(gitRun(t, "-C", root, "rev-parse", "HEAD")) != sha {
		t.Fatal("CLI mutated checkout")
	}
	state, _ := deps.StateHome()
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatal("inspection wrote machine-local state", err, entries)
	}
	for _, args := range [][]string{{"sync", "apply"}, {"sync", "bootstrap", "adopt-machine"}, {"sync", "inspect", "--yes"}} {
		deps.Out, deps.Err = &bytes.Buffer{}, &bytes.Buffer{}
		if Execute(context.Background(), append([]string{"--profile", root}, args...), deps) == 0 {
			t.Fatal("execution surface available", args)
		}
	}
}
