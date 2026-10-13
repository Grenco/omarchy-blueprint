package screens

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"github.com/charmbracelet/x/ansi"
)

// denseRestorePlan has everything at once: blocked categories with many
// causes, requirements, a deferral, long operation labels and many skips.
func denseRestorePlan() model.RestorePlan {
	plan := freshMachineScreenPlan(150)
	for i := range 6 {
		plan.Compatibility.Categories[1].Findings = append(plan.Compatibility.Categories[1].Findings, model.CompatibilityFinding{Code: "services.unit.invalid", Target: fmt.Sprintf("unit-%d.service", i), State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Summary: fmt.Sprintf("systemd rejected this service: %s %d", strings.Repeat("reason ", 12), i)})
	}
	plan.Requirements = append(plan.Requirements, model.Requirement{ID: "x.extra", Provider: "themes", Reason: strings.Repeat("an external readiness step is needed ", 4), Remediation: []string{"omarchy", "theme", "refresh"}})
	plan.Deferred = []string{"hooks", "resources"}
	for i := range 40 {
		plan.Operations = append(plan.Operations, model.Operation{Provider: "config", Resource: "config:" + strings.Repeat("deeply/nested/", 6) + fmt.Sprint(i), Action: "write", Risk: model.RiskMedium, Label: "a long step label " + strings.Repeat("x", 60)})
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "hooks", Resource: fmt.Sprintf("hook:%02d-%s", i, strings.Repeat("y", 50)), Reason: "restore disabled for machine \"workstation\""})
	}
	for i := range 30 {
		plan.ActivationReview = append(plan.ActivationReview, model.ActivationCandidate{Unit: fmt.Sprintf("app-%02d-%s.service", i, strings.Repeat("z", 60)), Reason: "enabled on the source machine"})
	}
	return plan
}

func restoreInState(state string, width, height int) *Restore {
	screen := NewRestore(nil)
	screen.width, screen.height = width, height
	screen.options = policy.RestoreOptions{Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceExact}
	screen.override = true
	screen.current = denseRestorePlan()
	switch state {
	case "plan-last-item":
		screen.selected = screen.currentEntryCount() - 1
	case "last-run":
		screen.Update(failedRun(25))
		screen.planning = false
		screen.current = denseRestorePlan()
	case "activation":
		screen.reviewingActivation = true
		screen.activationSelected = len(screen.current.ActivationReview) - 1
	case "applying":
		screen.busy, screen.stopRequested = true, true
		screen.run = &restoreRunState{total: 400}
		screen.run.record(restore.Progress{Type: restore.ProgressStarted, Operation: model.Operation{Resource: "config:" + strings.Repeat("very/long/", 20), Action: "write"}})
		screen.run.record(restore.Progress{Type: restore.ProgressHeartbeat, Elapsed: 95 * time.Second})
		screen.run.record(restore.Progress{Type: restore.ProgressFailed})
		screen.run.record(restore.Progress{Type: restore.ProgressSkipped})
	case "error":
		screen.err = errors.New("inspect services: " + strings.Repeat("the user manager could not be reached ", 10))
	}
	return screen
}

// Every Restore state stays within the workspace at every size, including
// after a resize, and the selection is never pushed out of view.
func TestRestoreEveryStateFitsAtEverySize(t *testing.T) {
	for _, state := range []string{"plan-last-item", "last-run", "activation", "applying", "error"} {
		for _, size := range workspaceSizes {
			t.Run(fmt.Sprintf("%s/%dx%d", state, size.width, size.height), func(t *testing.T) {
				screen := restoreInState(state, size.width, size.height)
				assertFits(t, "Restore "+state, screen.View(), size.width, size.height)
				for _, resized := range workspaceSizes {
					screen.SetSize(resized.width, resized.height)
					assertFits(t, "Restore "+state+" resized", screen.View(), resized.width, resized.height)
				}
			})
		}
	}
}

func TestRestoreSelectionStaysVisibleInDensePlans(t *testing.T) {
	for _, size := range workspaceSizes {
		screen := restoreInState("plan-last-item", size.width, size.height)
		view := ansi.Strip(screen.View())
		if !strings.Contains(view, "hook:39") {
			t.Fatalf("%dx%d: the selected last skip is not visible:\n%s", size.width, size.height, view)
		}
		screen = restoreInState("activation", size.width, size.height)
		if view = ansi.Strip(screen.View()); !strings.Contains(view, "app-29") {
			t.Fatalf("%dx%d: the selected last activation is not visible:\n%s", size.width, size.height, view)
		}
	}
}

var _ = tea.KeyPressMsg{}
var _ workflow.RestoreResult
