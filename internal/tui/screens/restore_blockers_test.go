package screens

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

func freshMachineScreenPlan(packages int) model.RestorePlan {
	pkgs := model.CompatibilityCategory{Category: "packages", Applies: true, State: model.CompatibilityUnknown, Authority: model.CompatibilityBlocked}
	for i := range packages {
		pkgs.Findings = append(pkgs.Findings, model.CompatibilityFinding{Code: "packages.origin.unknown", Target: fmt.Sprintf("official:pkg-%03d", i), State: model.CompatibilityUnknown, Authority: model.CompatibilityBlocked, Summary: "pacman's package databases aren't set up yet", RequirementID: "packages.metadata"})
	}
	return model.RestorePlan{
		Compatibility: model.CompatibilityReport{Categories: []model.CompatibilityCategory{
			pkgs,
			{Category: "services", Applies: true, State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Findings: []model.CompatibilityFinding{{Code: "services.unit.invalid", Target: "waybar-extra.service", State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Summary: "systemd rejected this service: Command /usr/bin/waybar-extra is not executable"}}},
			{Category: "themes", Applies: true, State: model.CompatibilitySupported, Authority: model.CompatibilityUnchanged, Evidence: []model.CompatibilityEvidence{{Kind: "builtin", Summary: "built-in theme"}}},
		}},
		Requirements: []model.Requirement{{ID: "packages.metadata", Provider: "packages", Reason: "pacman's package databases aren't set up on this machine yet", Remediation: []string{"omarchy", "update"}}},
		Operations:   []model.Operation{{Provider: "themes", Resource: "theme:tokyo-night", Action: "install", Risk: model.RiskLow}},
	}
}

func TestRestoreBlockedFreshMachineKeepsThePlanOnScreen(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = 120, 40
	screen.current = freshMachineScreenPlan(200)
	view := screen.View()
	assertWithinBudget(t, screen, view)
	for _, want := range []string{"Compatibility: Blocked · 2 categories not ready", "Packages · Unknown · Blocked · 200 targets", "Fix: run `omarchy update`", "Command /usr/bin/waybar-extra is not executable", "press d to restore everything else now and leave Packages, Services", "theme:tokyo-night"} {
		if !strings.Contains(view, want) {
			t.Fatalf("blocked fresh-machine view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "pkg-150") {
		t.Fatalf("blocked view enumerates every target:\n%s", view)
	}
	lines := screen.blockerLines(120, 2)
	if len(lines) > 10 {
		t.Fatalf("blocker section is %d lines; it must stay bounded:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func TestRestoreBlockerSectionStaysBoundedWithManyCauses(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = 120, 40
	plan := freshMachineScreenPlan(1)
	for i := range 10 {
		plan.Compatibility.Categories[1].Findings = append(plan.Compatibility.Categories[1].Findings, model.CompatibilityFinding{Code: "services.unit.invalid", Target: fmt.Sprintf("unit-%d.service", i), State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Summary: fmt.Sprintf("systemd rejected this service: reason %d", i)})
	}
	screen.current = plan
	lines := screen.blockerLines(120, 2)
	if len(lines) > 14 || !strings.Contains(strings.Join(lines, "\n"), "more causes (v details)") {
		t.Fatalf("many causes were not bounded (%d lines):\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func TestRestoreDeferKeyNarrowsToReadyCategoriesAndTogglesBack(t *testing.T) {
	screen := NewRestore(nil)
	screen.current = freshMachineScreenPlan(3)
	if !screen.CanDefer() {
		t.Fatal("blocked plan with ready categories cannot be deferred")
	}
	screen.Update(tea.KeyPressMsg{Code: 'd'})
	if !reflect.DeepEqual(screen.deferred, []string{"packages", "services"}) || !reflect.DeepEqual(screen.scope().Defer, []string{"packages", "services"}) {
		t.Fatalf("d deferred %v, want the categories that aren't ready", screen.deferred)
	}
	screen.current = model.RestorePlan{Deferred: []string{"packages", "services"}, Operations: []model.Operation{{Provider: "themes", Resource: "theme:tokyo-night", Action: "install"}}}
	screen.width, screen.height = 120, 40
	if view := screen.View(); !strings.Contains(view, "Deferred for later: Packages, Services") || !strings.Contains(view, "press d to plan them again") {
		t.Fatalf("deferred plan does not say what it leaves out:\n%s", view)
	}
	if screen.CanDefer() {
		t.Fatal("an already deferred plan offers to defer again")
	}
	screen.Update(tea.KeyPressMsg{Code: 'd'})
	if screen.deferred != nil {
		t.Fatalf("second d did not clear the deferral: %v", screen.deferred)
	}
}

func TestRestoreCannotDeferEverything(t *testing.T) {
	screen := NewRestore(nil)
	plan := freshMachineScreenPlan(1)
	plan.Compatibility.Categories = plan.Compatibility.Categories[:2]
	screen.current = plan
	if screen.CanDefer() {
		t.Fatal("deferral offered although nothing would be left to restore")
	}
	if strings.Contains(strings.Join(screen.nextStepLines(120), "\n"), "press d") {
		t.Fatal("next steps mention d although it does nothing")
	}
}

func TestRestoreBusyViewShowsTheRunningStep(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = 100, 20
	screen.busy = true
	screen.progress = restoreProgress{total: 5}
	events := make(chan restore.Progress)
	close(events)
	notes := model.Operation{Provider: "resources", Resource: "resource:notes", Action: "git clone"}
	for _, event := range []restore.Progress{
		{Type: restore.ProgressStarted, Operation: model.Operation{Resource: "theme:a", Action: "install"}},
		{Type: restore.ProgressCompleted, Operation: model.Operation{Resource: "theme:a", Action: "install"}},
		{Type: restore.ProgressStarted, Operation: notes},
		{Type: restore.ProgressHeartbeat, Operation: notes, Elapsed: 12 * time.Second},
	} {
		screen.Update(restoreProgressMsg{event: event, events: events})
	}
	view := screen.View()
	for _, want := range []string{"Applying restore · 1 of 5 steps done", "Now: Git clone resource:notes · 12s", "stops with an error"} {
		if !strings.Contains(view, want) {
			t.Fatalf("busy view lacks %q:\n%s", want, view)
		}
	}
}

func TestRestoreTerminalProgressLines(t *testing.T) {
	op := model.Operation{Resource: "official:firefox", Action: "install"}
	for _, tc := range []struct {
		event restore.Progress
		want  string
	}{
		{restore.Progress{Type: restore.ProgressStarted, Operation: op}, "→ Install official:firefox"},
		{restore.Progress{Type: restore.ProgressCompleted, Operation: op, Elapsed: 3 * time.Second}, "✓ Install official:firefox (3s)"},
		{restore.Progress{Type: restore.ProgressFailed, Operation: op, Elapsed: time.Second}, "✗ Install official:firefox failed after 1s"},
		{restore.Progress{Type: restore.ProgressHeartbeat, Operation: op, Elapsed: 30 * time.Second}, "  still running: Install official:firefox (30s)"},
		{restore.Progress{Type: restore.ProgressHeartbeat, Operation: op, Elapsed: 10 * time.Second}, ""},
	} {
		if got := restoreProgressLine(tc.event); got != tc.want {
			t.Fatalf("restoreProgressLine(%s, %s) = %q, want %q", tc.event.Type, tc.event.Elapsed, got, tc.want)
		}
	}
}
