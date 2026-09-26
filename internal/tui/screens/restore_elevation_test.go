package screens

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func readinessPlan() model.RestorePlan {
	return model.RestorePlan{
		Operations:   []model.Operation{{ID: "packages.install.official", Provider: "packages", Resource: "official:alacritty", Action: "install", Command: []string{"omarchy", "pkg", "add", "alacritty"}, Interactive: true, Notice: "May ask for administrator authentication (sudo) in this terminal."}},
		Requirements: []model.Requirement{{ID: "packages.metadata", Provider: "packages", Kind: "package-metadata", Reason: "package metadata unavailable", Remediation: []string{"omarchy", "update"}, Operations: []string{"packages.install.official"}}},
	}
}

func TestRestoreShowsReadinessRequirementAndRefusesToApply(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = 100, 40
	screen.current = readinessPlan()
	if screen.CanApply() {
		t.Fatal("a plan with an unmet requirement is applicable")
	}
	if view := screen.View(); !strings.Contains(view, "Requires before applying") || !strings.Contains(view, "omarchy update") {
		t.Fatalf("readiness requirement not shown:\n%s", view)
	}
	if cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || screen.confirm {
		t.Fatalf("unmet requirement opened confirmation: cmd=%v confirm=%t", cmd != nil, screen.confirm)
	}
}

func TestRestoreHandsTheTerminalToApprovedInteractiveRestore(t *testing.T) {
	screen := NewRestore(nil)
	plan := readinessPlan()
	plan.Requirements = nil
	screen.current = plan
	var handed tea.ExecCommand
	screen.exec = func(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		handed = c
		return func() tea.Msg { return fn(nil) }
	}
	if !screen.CanApply() {
		t.Fatal("an interactive plan must be applicable from the TUI")
	}
	if modal := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); modal == nil {
		t.Fatal("no confirmation requested")
	}
	if confirmation := screen.confirmation(); !strings.Contains(confirmation, "administrator authentication") {
		t.Fatalf("confirmation hides the terminal authentication step: %s", confirmation)
	}
	if cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("approval dispatched nothing")
	}
	command, ok := handed.(*restoreTerminalCommand)
	if !ok {
		t.Fatalf("interactive restore did not hand over the terminal: %#v", handed)
	}
	if !reflect.DeepEqual(command.approved, plan) {
		t.Fatalf("terminal restore does not apply the approved plan: %#v", command.approved)
	}
}

func TestRestoreKeepsNonInteractivePlansInsideTheTUI(t *testing.T) {
	screen := NewRestore(nil)
	screen.current.Operations = []model.Operation{{ID: "write", File: &model.FileWrite{ExpectedMissing: true}}}
	screen.exec = func(tea.ExecCommand, tea.ExecCallback) tea.Cmd {
		t.Fatal("non-interactive restore released the terminal")
		return nil
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("approval dispatched nothing")
	} else if msg, ok := cmd().(restoreAppliedMsg); !ok || msg.err == nil {
		t.Fatalf("expected in-TUI apply result, got %#v", msg)
	}
}
