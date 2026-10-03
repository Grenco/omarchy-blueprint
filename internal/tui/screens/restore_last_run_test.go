package screens

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func failedRun(failures int) restoreAppliedMsg {
	result := workflow.RestoreResult{Applied: true, Journal: "/state/restores/restore-1.jsonl"}
	for i := range failures {
		result.Execution.Failed = append(result.Execution.Failed, restore.Failure{Operation: model.Operation{Resource: fmt.Sprintf("theme:t%02d", i), Action: "remove"}, Error: fmt.Sprintf("omarchy theme remove t%02d: exit status 1", i)})
	}
	result.Execution.Completed = []model.Operation{{Resource: "theme:kept"}}
	result.Execution.Blocked = []restore.Blocked{{Operation: model.Operation{Resource: "config:after"}, Dependency: "theme.remove.t00"}}
	return restoreAppliedMsg{result: result, err: fmt.Errorf("restore completed with %d failed operation(s)", failures)}
}

func TestFailedApplyIsReportedAboveThePlanNotInsteadOfIt(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = 120, 40
	screen.current = model.RestorePlan{Operations: []model.Operation{{Provider: "themes", Resource: "theme:t00", Action: "remove"}}}
	screen.Update(failedRun(2))
	if screen.err != nil {
		t.Fatalf("an apply outcome became a planning error: %v", screen.err)
	}
	view := screen.View()
	for _, want := range []string{"Last restore: 2 steps failed, 1 completed (o details)", "✗ Remove theme:t00: omarchy theme remove t00: exit status 1", "theme:t00"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	screen.Update(tea.KeyPressMsg{Code: 'o'})
	detail := screen.DetailView()
	for _, want := range []string{"✗ Remove theme:t01: omarchy theme remove t01: exit status 1", "Held back", "config:after", "Journal: /state/restores/restore-1.jsonl"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("details lack %q:\n%s", want, detail)
		}
	}
}

func TestManyFailuresStayBoundedAboveThePlan(t *testing.T) {
	screen := NewRestore(nil)
	screen.Update(failedRun(15))
	lines := screen.lastRunLines(120, restoreLastRunLines)
	if len(lines) > restoreLastRunLines || !strings.Contains(strings.Join(lines, "\n"), "more (o details)") {
		t.Fatalf("15 failures were not bounded (%d lines):\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func TestSuccessfulAndSkippedRunsSayWhatHappened(t *testing.T) {
	screen := NewRestore(nil)
	screen.Update(restoreAppliedMsg{result: workflow.RestoreResult{Applied: true, Verification: model.VerificationResult{OK: true}}})
	if got := strings.Join(screen.lastRunLines(120, restoreLastRunLines), "\n"); !strings.Contains(got, "applied and verified") {
		t.Fatalf("successful run: %q", got)
	}
	skipped := workflow.RestoreResult{Applied: true}
	skipped.Execution.SkippedByYou = []model.Operation{{Label: "Omarchy's Tailscale setup"}}
	screen.Update(restoreAppliedMsg{result: skipped})
	if got := strings.Join(screen.lastRunLines(120, restoreLastRunLines), "\n"); !strings.Contains(got, "except what you skipped") || !strings.Contains(got, "Skipped by you: Omarchy's Tailscale setup") {
		t.Fatalf("skipped run: %q", got)
	}
	screen.Update(restoreAppliedMsg{err: workflow.ErrRestorePlanChanged})
	if got := strings.Join(screen.lastRunLines(120, restoreLastRunLines), "\n"); !strings.Contains(got, "didn't run: restore plan changed after approval") {
		t.Fatalf("refused run: %q", got)
	}
}

func TestPlanningErrorOnlyRespondsToReplan(t *testing.T) {
	screen := NewRestore(nil)
	screen.current = model.RestorePlan{Operations: []model.Operation{{Resource: "a"}, {Resource: "b"}}}
	screen.err = errors.New("inspect themes: boom")
	screen.Update(tea.KeyPressMsg{Code: 'j'})
	if screen.selected != 0 {
		t.Fatal("a hidden plan was navigated behind the planning error")
	}
	if view := screen.View(); !strings.Contains(view, "Press r to plan again") {
		t.Fatalf("planning error gives no way forward:\n%s", view)
	}
}
