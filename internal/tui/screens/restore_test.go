package screens

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

func TestRestoreExplainsWhenNothingHasEverBeenCaptured(t *testing.T) {
	session, _ := newSyncSession(t)
	screen := NewRestore(session)

	view := screen.View()
	for _, want := range []string{
		"Nothing to restore yet",
		"Restore recreates state that is already saved",
		"Capture the parts of this machine",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("fresh Restore missing %q:\n%s", want, view)
		}
	}
}

func TestRestoreCapturedButCleanKeepsNormalZeroOperationState(t *testing.T) {
	session, _ := newSyncSession(t)
	if err := session.SetProviderCaptured(context.Background(), "hooks", true); err != nil {
		t.Fatal(err)
	}
	screen := NewRestore(session)

	view := screen.View()
	if strings.Contains(view, "Nothing to restore yet") {
		t.Fatalf("captured clean profile shown as uncaptured:\n%s", view)
	}
	if !strings.Contains(view, "No restore operations required.") {
		t.Fatalf("captured clean restore lost normal zero-state:\n%s", view)
	}
}

func TestRestoreUsesOneCurrentPlanAndShowsExactSafetyAt80Columns(t *testing.T) {
	session, _ := newSyncSession(t)
	if err := session.SetProviderCaptured(context.Background(), "hooks", true); err != nil {
		t.Fatal(err)
	}
	screen := NewRestore(session)
	screen.width = 80
	screen.current = model.RestorePlan{Operations: []model.Operation{{Provider: "hooks", Resource: "old hook", Delete: &model.FileDelete{}}}}
	screen.options = policy.RestoreOptions{Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceExact}
	screen.override = true
	view := screen.View()
	for _, want := range []string{"Conflicts: safe", "Convergence: exact", "One-run override active", "WARNING: Exact", "delete"} {
		if !strings.Contains(view, want) {
			t.Fatalf("current restore plan missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "NORMAL   FORCED") {
		t.Fatalf("legacy comparison matrix leaked into current plan:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > 80 {
			t.Fatalf("current-plan safety line is %d columns wide:\n%s", width, view)
		}
	}
}

func TestRestoreConfirmationReportsCurrentAuthorityAndDestructiveCounts(t *testing.T) {
	session, _ := newSyncSession(t)
	screen := NewRestore(session)
	screen.options = policy.RestoreOptions{Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceExact}
	screen.override = true
	screen.forcedOverrides = 2
	screen.current = model.RestorePlan{
		Operations: []model.Operation{
			{Provider: "config", Resource: "old file", Action: "delete", Delete: &model.FileDelete{}},
			{Provider: "packages", Resource: "mise:node", Action: "remove", Command: []string{"mise", "uninstall"}, Risk: model.RiskHigh},
		},
		Skipped: []model.Skipped{{Provider: "packages", Resource: "official:git", Reason: "restore disabled"}, {Provider: "config", Resource: "conflict", Reason: "overwrite disabled"}},
	}
	confirmation := screen.confirmation()
	for _, want := range []string{"conflicts:force", "convergence:exact", "removals:2", "policy-skips:1", "forced-overrides:2"} {
		if !strings.Contains(confirmation, want) {
			t.Fatalf("confirmation missing %q: %s", want, confirmation)
		}
	}
	if strings.Contains(confirmation, "normal mode") {
		t.Fatalf("confirmation used legacy mode: %s", confirmation)
	}
}

func TestRestoreCurrentPlanSelectionDrivesDetails(t *testing.T) {
	screen := NewRestore(nil)
	screen.current = model.RestorePlan{
		Operations: []model.Operation{{Provider: "config", Resource: "one", Action: "write", Risk: model.RiskLow}},
		Skipped:    []model.Skipped{{Provider: "packages", Resource: "two", Reason: "restore disabled"}},
	}
	if detail := screen.DetailView(); !strings.Contains(detail, "Restore operation") || !strings.Contains(detail, "Resource: one") {
		t.Fatalf("first current-plan detail = %q", detail)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if detail := screen.DetailView(); !strings.Contains(detail, "Restore skip") || !strings.Contains(detail, "Resource: two") || !strings.Contains(detail, "restore disabled") {
		t.Fatalf("selected current-plan detail = %q", detail)
	}
}

func TestRestoreSanitizesCurrentPlanAndErrors(t *testing.T) {
	screen := NewRestore(nil)
	screen.current.Operations = []model.Operation{{Provider: "bad\nprovider\x1b", Resource: "bad\nresource\x1b"}}
	if view := screen.View(); strings.Contains(view, "\x1b") || !strings.Contains(view, "bad?resource?") {
		t.Fatalf("unsafe restore=%q", view)
	}
	if detail := screen.DetailView(); strings.Contains(detail, "\x1b") || !strings.Contains(detail, "bad?provider?") {
		t.Fatalf("unsafe detail=%q", detail)
	}
	screen.err = errors.New("bad\nerror\x1b")
	if view := screen.View(); strings.Contains(view, "\x1b") || !strings.Contains(view, "bad?error?") {
		t.Fatalf("unsafe error=%q", view)
	}
}

func TestRestoreOneRunControlsAreIndependent(t *testing.T) {
	screen := NewRestore(nil)
	screen.Update(tea.KeyPressMsg{Code: 'f'})
	if screen.options.Conflicts != policy.ConflictForce || screen.options.Convergence != policy.ConvergenceAdditive || !screen.override {
		t.Fatalf("Force toggle changed wrong axes: %+v override=%t", screen.options, screen.override)
	}
	screen.Update(tea.KeyPressMsg{Code: 'e'})
	if screen.options.Conflicts != policy.ConflictForce || screen.options.Convergence != policy.ConvergenceExact {
		t.Fatalf("Exact toggle changed wrong axes: %+v", screen.options)
	}
}

func TestRestoreScreenConfirmationDispatchesOneMutation(t *testing.T) {
	screen := NewRestore(nil)
	screen.current.Operations = []model.Operation{{ID: "write", File: &model.FileWrite{ExpectedMissing: true}}}
	modal := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})()
	if modal == nil {
		t.Fatal("restore did not request shared confirmation modal")
	}
	first := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	second := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if first == nil || second != nil || !screen.busy || screen.confirm {
		t.Fatalf("restore confirmation dispatched more than once: first=%v second=%v busy=%v confirm=%v", first != nil, second != nil, screen.busy, screen.confirm)
	}
}

func TestRestoreCurrentPlanLongPathIsBoundedAt80Columns(t *testing.T) {
	screen := NewRestore(nil)
	screen.width = 80
	screen.current.Operations = []model.Operation{{Provider: "config", Resource: "~/.config/omarchy/current/theme/waybar-status-bar-layout.jsonc", Risk: model.RiskMedium}}
	view := screen.View()
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 80 {
			t.Fatalf("line overflows 80 columns: got %d:\n%q", got, line)
		}
	}
}
