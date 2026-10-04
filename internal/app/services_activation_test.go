package app

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	services "github.com/Grenco/omarchy-blueprint/internal/providers/services"
	tuiscreens "github.com/Grenco/omarchy-blueprint/internal/tui/screens"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type activationRunner struct {
	base    *machineRunner
	manager *appServicesSystemd
	starts  []string
}

func (r *activationRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "systemctl" && len(args) == 4 && args[0] == "--user" && args[1] == "start" && args[2] == "--" {
		r.starts = append(r.starts, args[3])
		r.manager.units[0].ServiceType, r.manager.units[0].ExecutionResult = "oneshot", "success"
		return "", nil
	}
	return r.base.Run(ctx, name, args...)
}

func serviceActivationFixture(t *testing.T, preference profile.ServiceActivationPreference) (string, Dependencies, *activationRunner) {
	t.Helper()
	dir, deps, roots := servicesCLIFixture(t)
	path := filepath.Join(roots.UserConfigDir, "backup.service")
	if err := os.WriteFile(path, []byte("[Service]\nType=oneshot\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := &appServicesSystemd{units: []services.ObservedUnit{appService("backup.service", path)}}
	deps.ServicesSystemd = manager
	deps.In = strings.NewReader("yes\nyes\n")
	if code, out := configRun(t, deps, dir, "capture", "services", "--review"); code != 0 {
		t.Fatal(out)
	}
	data, err := profile.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	data.Services.Units[0].ObservedActive, data.Services.Units[0].ActivationPreference = true, preference
	if err := profile.Save(dir, data); err != nil {
		t.Fatal(err)
	}
	runner := &activationRunner{base: deps.Runner.(*machineRunner), manager: manager}
	deps.Runner = runner
	return dir, deps, runner
}

func TestHeadlessRestoreDoesNotImplicitlyActivate(t *testing.T) {
	dir, deps, runner := serviceActivationFixture(t, profile.ServiceActivationRestoreWorkingState)
	deps.IsTTY = func() bool { return false }
	if code, out := configRun(t, deps, dir, "restore", "services", "--yes"); code != 0 || len(runner.starts) != 0 {
		t.Fatalf("implicit activation: code=%d out=%s starts=%v", code, out, runner.starts)
	}
	if code, out := configRun(t, deps, dir, "restore", "services", "--activation", "working", "--yes"); code != 0 || len(runner.starts) != 1 {
		t.Fatalf("explicit working oneshot failed: code=%d out=%s starts=%v", code, out, runner.starts)
	}
}

func TestReviewPreferenceNeverSweptIntoHeadlessWorking(t *testing.T) {
	dir, deps, runner := serviceActivationFixture(t, profile.ServiceActivationReview)
	deps.IsTTY = func() bool { return false }
	if code, out := configRun(t, deps, dir, "restore", "services", "--activation", "working", "--yes"); code != 0 || len(runner.starts) != 0 || !strings.Contains(out, "interactive activation review") {
		t.Fatalf("working swept up review preference: code=%d out=%s starts=%v", code, out, runner.starts)
	}
	if code, out := configRun(t, deps, dir, "restore", "services", "--activation", "review", "--yes"); code == 0 || !strings.Contains(out, "interactive terminal") || len(runner.starts) != 0 {
		t.Fatalf("headless review did not fail closed: code=%d out=%s", code, out)
	}
}

func TestCLIReviewActivationIndividuallyApprovesStarts(t *testing.T) {
	for _, approve := range []bool{false, true} {
		dir, deps, runner := serviceActivationFixture(t, profile.ServiceActivationReview)
		answer := "no\n"
		if approve {
			answer = "yes\n"
		}
		deps.In = strings.NewReader(answer + "yes\n")
		code, out := configRun(t, deps, dir, "restore", "services", "--activation", "review")
		if code != 0 || (len(runner.starts) == 1) != approve || !strings.Contains(out, "Activate backup.service after Restore?") {
			t.Fatalf("individual review failed: approve=%t code=%d out=%s starts=%v", approve, code, out, runner.starts)
		}
	}
}

func TestActivationApprovalRecalculationRejectsChangedCapturedEvidence(t *testing.T) {
	dir, deps, runner := serviceActivationFixture(t, profile.ServiceActivationRestoreWorkingState)
	session, err := openWorkflow(deps, &options{profileDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	opts := policy.DefaultRestoreOptions()
	opts.Activation = policy.ActivationRestoreWorkingState
	approved, err := session.PlanRestore(context.Background(), "services", &opts)
	if err != nil {
		t.Fatal(err)
	}
	data, err := profile.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	data.Services.Units[0].ObservedActive = false
	if err := profile.Save(dir, data); err != nil {
		t.Fatal(err)
	}
	_, err = session.ApplyApprovedRestore(context.Background(), "services", &opts, approved, false)
	if !errors.Is(err, workflow.ErrRestorePlanChanged) || len(runner.starts) != 0 {
		t.Fatalf("changed activation evidence retained approval: %v starts=%v", err, runner.starts)
	}
}

func TestReviewActivationDryRunJSONListsCandidatesWithoutAuthority(t *testing.T) {
	dir, deps, runner := serviceActivationFixture(t, profile.ServiceActivationReview)
	deps.IsTTY = func() bool { return false }
	code, out := configRun(t, deps, dir, "--json", "restore", "services", "--activation", "review", "--dry-run")
	if code != 0 || !strings.Contains(out, `"activation_review"`) || !strings.Contains(out, `"approved": false`) || strings.Contains(out, `"action": "start"`) || len(runner.starts) != 0 {
		t.Fatalf("review preview granted authority: code=%d out=%s", code, out)
	}
}

func TestServicesCLIAndTUIActivationPlanParity(t *testing.T) {
	for _, mode := range []string{"working", "review"} {
		t.Run(mode, func(t *testing.T) {
			dir, deps, _ := serviceActivationFixture(t, profile.ServiceActivationRestoreWorkingState)
			mise := filepath.Join(t.TempDir(), "mise.toml")
			deps.MiseGlobalConfig = func() (string, error) { return mise, nil }
			code, out := configRun(t, deps, dir, "--json", "restore", "--activation", mode, "--dry-run")
			if code != 0 {
				t.Fatal(out)
			}
			var envelope struct {
				Data struct {
					Plan model.RestorePlan `json:"plan"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(out), &envelope); err != nil {
				t.Fatal(err)
			}
			session, err := openWorkflow(deps, &options{profileDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			screen := tuiscreens.NewRestore(session)
			screen.Update(screen.Init()())
			cmd := screen.Update(tea.KeyPressMsg{Code: 'a'})
			screen.Update(cmd())
			if mode == "review" {
				cmd = screen.Update(tea.KeyPressMsg{Code: 'a'})
				screen.Update(cmd())
			}
			if !reflect.DeepEqual(envelope.Data.Plan, screen.CurrentPlan()) {
				t.Fatalf("CLI/TUI domain plans differ:\nCLI=%+v\nTUI=%+v", envelope.Data.Plan, screen.CurrentPlan())
			}
		})
	}
}

func TestTUIReviewApprovalReachesSharedExecutionAndVerify(t *testing.T) {
	dir, deps, runner := serviceActivationFixture(t, profile.ServiceActivationReview)
	mise := filepath.Join(t.TempDir(), "mise.toml")
	deps.MiseGlobalConfig = func() (string, error) { return mise, nil }
	session, err := openWorkflow(deps, &options{profileDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	screen := tuiscreens.NewRestore(session)
	screen.Update(screen.Init()())
	for i := 0; i < 2; i++ {
		cmd := screen.Update(tea.KeyPressMsg{Code: 'a'})
		screen.Update(cmd())
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen.Update(tea.KeyPressMsg{Code: ' '})
	cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	screen.Update(cmd())
	cmd = screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("approved start has no apply confirmation")
	}
	cmd() // modal presentation, then Enter approves the complete current plan
	cmd = screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := cmd()
	// In-interface apply is a batch: the apply itself and its progress
	// listener. Run them in order, as the program would deliver them.
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, part := range batch {
			if part != nil {
				if result := part(); result != nil {
					screen.Update(result)
				}
			}
		}
	} else {
		screen.Update(msg)
	}
	if len(runner.starts) != 1 || strings.Contains(screen.View(), "Unable") {
		t.Fatalf("TUI review was treated as headless or lost receipts: starts=%v view=%s", runner.starts, screen.View())
	}
}
