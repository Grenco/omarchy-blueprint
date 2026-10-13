package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

type syncFailureRunner struct {
	gitMachineRunner
	failAbort        bool
	unapprovedCommit bool
}

func (r *syncFailureRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "git" {
		for _, arg := range args {
			if arg == "commit" && r.unapprovedCommit {
				for i, v := range args {
					if v == "-C" && i+1 < len(args) {
						root := args[i+1]
						if err := os.WriteFile(filepath.Join(root, "unapproved.txt"), []byte("raw-secret-body"), 0600); err != nil {
							return "", err
						}
						if _, err := r.gitMachineRunner.Run(ctx, "git", "-C", root, "add", "unapproved.txt"); err != nil {
							return "", err
						}
					}
				}
				return r.gitMachineRunner.Run(ctx, name, args...)
			}
			if arg == "commit" || (r.failAbort && arg == "--abort") {
				return "raw-secret-body", errors.New("https://user:credential@host.invalid/private /private/profile")
			}
		}
	}
	return r.gitMachineRunner.Run(ctx, name, args...)
}

func TestSyncApplyReportsOrdinaryVersusRepairRequiredExecutionFailure(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		for _, scenario := range []string{"ordinary", "rollback", "post-commit"} {
			t.Run(fmt.Sprintf("json=%t/%s", asJSON, scenario), func(t *testing.T) {
				repair := scenario != "ordinary"
				root, deps := configSandbox(t)
				deps.IsTTY = func() bool { return false }
				runner := &syncFailureRunner{failAbort: scenario == "rollback", unapprovedCommit: scenario == "post-commit"}
				runner.official = map[string]bool{"zoxide": true, "local": true}
				runner.aur = map[string]bool{}
				deps.Runner = runner
				d, err := profile.Load(root)
				if err != nil {
					t.Fatal(err)
				}
				d.Manifest.Capture.Packages = true
				d.Packages.Official = []string{"zoxide"}
				d.Packages.Preinstalls.Managed = true
				d.Packages.Preinstalls.Items = map[string]bool{"aether": false, "libreoffice-fresh": false}
				d.Machines.Items = []profile.Machine{{Name: "desktop"}}
				if err := profile.Save(root, d); err != nil {
					t.Fatal(err)
				}
				gitRun(t, "-C", root, "init", "-b", "main")
				gitRun(t, "-C", root, "config", "user.email", "test@example.invalid")
				gitRun(t, "-C", root, "config", "user.name", "Blueprint Test")
				gitRun(t, "-C", root, "add", ".")
				gitRun(t, "-C", root, "commit", "-m", "baseline")
				origin := filepath.Join(t.TempDir(), "origin.git")
				gitRun(t, "init", "--bare", origin)
				gittest.ConfigureRemote(t, origin)
				gitRun(t, "-C", root, "remote", "add", "origin", origin)
				gitRun(t, "-C", root, "push", "-u", "origin", "main")
				service, err := profilegit.New(command.SystemRunner{}, root)
				if err != nil {
					t.Fatal(err)
				}
				info, err := service.CurrentRevisionInfo(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				state, _ := deps.StateHome()
				store := syncengine.CursorStore{StateHome: state, ProfileRoot: root}
				cursor := syncengine.Cursor{Version: 1, Machine: "desktop", Revision: info.SHA, Branch: info.Branch, Upstream: info.Upstream, RemoteFingerprint: info.OriginFingerprint}
				if err := store.Save(cursor); err != nil {
					t.Fatal(err)
				}
				peer := filepath.Join(t.TempDir(), "peer")
				gitRun(t, "clone", "-b", "main", origin, peer)
				gitRun(t, "-C", peer, "config", "user.email", "test@example.invalid")
				gitRun(t, "-C", peer, "config", "user.name", "Blueprint Test")
				d.Packages.Official = append(d.Packages.Official, "remote")
				if err := profile.Save(peer, d); err != nil {
					t.Fatal(err)
				}
				gitRun(t, "-C", peer, "add", ".")
				gitRun(t, "-C", peer, "commit", "-m", "remote")
				gitRun(t, "-C", peer, "push")
				d.Packages.Official = []string{"zoxide", "local"}
				if err := profile.Save(root, d); err != nil {
					t.Fatal(err)
				}
				gitRun(t, "-C", root, "add", ".")
				gitRun(t, "-C", root, "commit", "-m", "local")
				localSHA := strings.TrimSpace(gitRun(t, "-C", root, "rev-parse", "HEAD"))
				gitRun(t, "-C", root, "fetch")
				out, stderr := deps.Out.(*bytes.Buffer), deps.Err.(*bytes.Buffer)
				out.Reset()
				stderr.Reset()
				args := []string{"--profile", root, "--machine", "desktop", "sync", "apply", "--no-fetch", "--yes"}
				if asJSON {
					args = append(args, "--json")
				}
				if code := Execute(context.Background(), args, deps); code != 1 {
					t.Fatalf("failure exit=%d output=%s", code, out.String())
				}
				_, mergeErr := os.Stat(filepath.Join(root, ".git", "MERGE_HEAD"))
				if scenario == "rollback" && mergeErr != nil {
					t.Fatal("fixture did not exercise failed Git rollback")
				}
				if !repair && (!os.IsNotExist(mergeErr) || strings.TrimSpace(gitRun(t, "-C", root, "rev-parse", "HEAD")) != localSHA || strings.TrimSpace(gitRun(t, "-C", root, "status", "--porcelain")) != "") {
					t.Fatal("ordinary failure did not safely restore the repository")
				}
				wantCode, wantSeverity := "execution-stopped", "error"
				if scenario == "post-commit" {
					if !os.IsNotExist(mergeErr) || strings.TrimSpace(gitRun(t, "-C", root, "rev-parse", "HEAD")) == localSHA || strings.TrimSpace(gitRun(t, "-C", root, "show", "HEAD:unapproved.txt")) != "raw-secret-body" {
						t.Fatal("fixture did not exercise an unapproved committed tree")
					}
				}
				if repair {
					wantCode, wantSeverity = "repository-rollback-failed", "high"
				}
				if scenario == "post-commit" {
					wantCode = "repository-post-commit-verification-failed"
				}
				if asJSON {
					var envelope struct {
						OK   bool `json:"ok"`
						Data struct {
							Execution workflow.SyncExecutionResult `json:"execution"`
							Failure   struct {
								Code, Severity, Guidance string
								RepairRequired           bool `json:"repair_required"`
							} `json:"failure"`
						} `json:"data"`
					}
					if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
						t.Fatalf("invalid failure JSON: %v %s", err, out.String())
					}
					f := envelope.Data.Failure
					if envelope.OK || envelope.Data.Execution.CursorAdvanced || envelope.Data.Execution.Published || f.Code != wantCode || f.Severity != wantSeverity || f.RepairRequired != repair {
						t.Fatalf("failure classification lost: %s", out.String())
					}
					if repair && !strings.Contains(f.Guidance, "Do not retry Sync") {
						t.Fatal("missing repair-before-retry guidance")
					}
				} else {
					if !strings.Contains(out.String(), wantCode) {
						t.Fatalf("failure classification lost: %s %s", out.String(), stderr.String())
					}
					if repair && (!strings.Contains(out.String(), "HIGH SEVERITY") || !strings.Contains(out.String(), "Manually repair") || !strings.Contains(stderr.String(), "Do not retry Sync")) {
						t.Fatal("critical repair instructions suppressed")
					}
				}
				output := out.String() + stderr.String()
				for _, secret := range []string{"credential", "raw-secret-body", "/private/profile", root, origin} {
					if strings.Contains(output, secret) {
						t.Fatal("failure output exposed sensitive context")
					}
				}
				after, ok, err := store.Load()
				if err != nil || !ok || after != cursor {
					t.Fatal("failed execution advanced cursor")
				}
			})
		}
	}
}

func TestSyncApplyHasExplicitOneShotFlagsAndNoPersistedResolution(t *testing.T) {
	_, deps := configSandbox(t)
	cmd, _, err := newRoot(deps).Find([]string{"sync", "apply"})
	if err != nil || cmd.Name() != "apply" {
		t.Fatalf("missing apply: %v", err)
	}
	for _, flag := range []string{"keep-local", "accept-remote", "keep-machine", "keep-profile", "defer", "yes", "dry-run", "no-fetch", "exact", "force"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("missing flag %s", flag)
		}
	}
	for _, choice := range []string{"reconcile-upstream", "adopt-machine", "cancel"} {
		cmd, _, err := newRoot(deps).Find([]string{"sync", "bootstrap", choice})
		if err != nil || cmd.Name() != choice {
			t.Fatalf("missing bootstrap choice %s", choice)
		}
	}
}

func TestSyncApplyDryRunJSONAndBlockedExecutionDoNotWrite(t *testing.T) {
	root, deps := configSandbox(t)
	deps.IsTTY = func() bool { return false }
	before, err := os.ReadFile(filepath.Join(root, "profile.toml"))
	if err != nil {
		t.Fatal(err)
	}
	code, out := configRun(t, deps, root, "--json", "sync", "apply", "--no-fetch", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run failed: %s", out)
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Sync syncengine.Plan `json:"sync"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || !envelope.OK || len(envelope.Data.Sync.Blockers) == 0 {
		t.Fatalf("missing typed blocked inspection: %s (%v)", out, err)
	}
	if code, _ := configRun(t, deps, root, "sync", "apply", "--no-fetch", "--yes"); code == 0 {
		t.Fatal("blocked execution returned success")
	}
	after, err := os.ReadFile(filepath.Join(root, "profile.toml"))
	if err != nil || string(before) != string(after) {
		t.Fatal("preview/blocked execution wrote profile")
	}
	state, _ := deps.StateHome()
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatal("preview/blocked execution wrote local Sync state")
	}
	if strings.Contains(out, root) || strings.Contains(out, state) {
		t.Fatal("JSON leaked private paths")
	}
}

func TestSyncBootstrapCancelDoesNotOpenProfile(t *testing.T) {
	root, deps := configSandbox(t)
	code, out := configRun(t, deps, filepath.Join(root, "does-not-exist"), "--json", "sync", "bootstrap", "cancel")
	if code != 0 || !strings.Contains(out, `"cancelled": true`) {
		t.Fatalf("cancel: %d %s", code, out)
	}
	state, _ := deps.StateHome()
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatal("cancel created local state")
	}
}

func TestSyncResolutionParsingPreservesProviderIdentityAndRejectsAmbiguity(t *testing.T) {
	choices, err := parseSyncChoices(map[string][]string{"keep-local": {"packages:official:git"}, "keep-machine": {"packages:official:git"}})
	if err != nil || len(choices) != 1 || choices[0].Key.Target != "official:git" || choices[0].Resolution != syncengine.ResolutionKeepLocal || choices[0].MachineResolution != syncengine.MachineResolutionKeepMachine {
		t.Fatalf("choices=%#v error=%v", choices, err)
	}
	for _, flags := range []map[string][]string{
		{"keep-local": {"packages:official:git", "packages:official:git"}},
		{"keep-local": {"packages:official:git"}, "accept-remote": {"packages:official:git"}},
		{"keep-machine": {"packages:official:git"}, "keep-profile": {"packages:official:git"}},
		{"keep-local": {"missing-separator"}},
	} {
		if _, err := parseSyncChoices(flags); err == nil {
			t.Fatal("accepted ambiguous choices")
		}
	}
}

func TestSyncParsedChoicesMustNameAnActualMeaningfulConflict(t *testing.T) {
	state := syncengine.State{Kind: syncengine.StatePresent, Fingerprint: "canonical"}
	key := syncengine.TargetKey{Category: "packages", Target: "official:git"}
	plan, err := syncengine.Reconcile(syncengine.PlanRequest{
		Identity: syncengine.Identity{Base: strings.Repeat("a", 40), Local: strings.Repeat("a", 40), Remote: strings.Repeat("a", 40), Machine: "desktop", Branch: "main", Upstream: "origin/main", RemoteFingerprint: strings.Repeat("b", 64), ControlFingerprint: strings.Repeat("c", 64)},
		Targets:  []syncengine.TargetInput{{Key: key, Base: state, Local: state, Remote: state, Machine: state}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, flags := range []map[string][]string{
		{"keep-local": {"packages:official:missing"}},
		{"keep-local": {"packages:official:git"}},
		{"keep-machine": {"packages:official:git"}},
	} {
		choices, err := parseSyncChoices(flags)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := workflow.ResolveSyncPlan(plan, choices); err == nil {
			t.Fatal("unknown or meaningless choice accepted")
		}
	}
}

func TestSyncApplyWithoutApprovalRejectsBeforeObservation(t *testing.T) {
	_, deps := configSandbox(t)
	deps.IsTTY = func() bool { return false }
	cmd := newRoot(deps)
	cmd.SetArgs([]string{"--json", "sync", "apply", "--no-fetch"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("noninteractive execution did not require approval: %v", err)
	}
}
