package screens

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

// An 80x24 terminal leaves Restore a 78x16 workspace once the header, rules,
// footer, panel borders and screen description are accounted for.
const (
	restoreCompactWidth  = 78
	restoreCompactHeight = 16
)

func exactRestore(operations []model.Operation, skipped []model.Skipped) *Restore {
	screen := NewRestore(nil)
	screen.width, screen.height = restoreCompactWidth, restoreCompactHeight
	screen.options = policy.RestoreOptions{Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceExact}
	screen.override = true
	screen.current = model.RestorePlan{Operations: operations, Skipped: skipped}
	return screen
}

func assertWithinBudget(t *testing.T, s *Restore, view string) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > s.height {
		t.Fatalf("Restore emitted %d lines for a %d-line workspace; it must not rely on Panel clipping:\n%s", len(lines), s.height, view)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > s.width {
			t.Fatalf("Restore line is %d columns in a %d-column workspace: %q", w, s.width, line)
		}
	}
}

func lineContaining(view, needle string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func TestRestoreAt80x24ShowsExactWarningRemovalAndSkip(t *testing.T) {
	screen := exactRestore(
		[]model.Operation{{Provider: "packages", Resource: "official:spotify", Action: "remove", Command: []string{"pacman", "-R"}, Risk: model.RiskHigh}},
		[]model.Skipped{{Provider: "config", Resource: ".config/example", Reason: "restore disabled by profile policy"}},
	)
	view := screen.View()
	assertWithinBudget(t, screen, view)
	if !strings.Contains(view, "Exact") || !strings.Contains(view, "Resource data is never deleted") {
		t.Fatalf("Exact safety warning missing:\n%s", view)
	}
	removal := lineContaining(view, "official:spotify")
	if !strings.Contains(removal, "Remove") || !strings.Contains(removal, "High") {
		t.Fatalf("removal row must show its action and risk, got %q:\n%s", removal, view)
	}
	skip := lineContaining(view, ".config/example")
	if !strings.Contains(skip, "Policy: Skip") {
		t.Fatalf("skip row must show why it is skipped, got %q:\n%s", skip, view)
	}
}

func longPlan() ([]model.Operation, []model.Skipped) {
	operations := make([]model.Operation, 0, 21)
	for i := 0; i < 20; i++ {
		operations = append(operations, model.Operation{Provider: "config", Resource: fmt.Sprintf("op-%02d", i), Action: "write", Risk: model.RiskLow})
	}
	operations = append(operations, model.Operation{Provider: "packages", Resource: "official:final-removal", Action: "remove", Risk: model.RiskHigh})
	skipped := make([]model.Skipped, 0, 5)
	for i := 0; i < 5; i++ {
		skipped = append(skipped, model.Skipped{Provider: "hooks", Resource: fmt.Sprintf("skip-%02d", i), Reason: "hardware target blocked for this machine"})
	}
	return operations, skipped
}

func selectedTarget(s *Restore) string {
	if s.selected < len(s.current.Operations) {
		return s.current.Operations[s.selected].Resource
	}
	return s.current.Skipped[s.selected-len(s.current.Operations)].Resource
}

func TestRestoreScrollsLongPlanAndKeepsSelectionVisibleAcrossSections(t *testing.T) {
	screen := exactRestore(longPlan())
	total := screen.currentEntryCount()

	visit := func(step string) {
		t.Helper()
		view := screen.View()
		assertWithinBudget(t, screen, view)
		target := selectedTarget(screen)
		line := lineContaining(view, target)
		if line == "" {
			t.Fatalf("%s: selected %q (index %d) is not visible:\n%s", step, target, screen.selected, view)
		}
		section := "Changes"
		if screen.selected >= len(screen.current.Operations) {
			section = "Skipped"
		}
		if !strings.Contains(view, section) {
			t.Fatalf("%s: selected %q lost its %s context:\n%s", step, target, section, view)
		}
		if !strings.Contains(view, "Exact") {
			t.Fatalf("%s: Exact warning scrolled away:\n%s", step, view)
		}
	}

	visit("initial")
	for i := 1; i < total; i++ {
		screen.Update(tea.KeyPressMsg{Code: 'j'})
		if screen.selected != i {
			t.Fatalf("j moved selection to %d, want %d", screen.selected, i)
		}
		visit(fmt.Sprintf("down to %d", i))
	}
	removal := lineContaining(func() string {
		for screen.selected != len(screen.current.Operations)-1 {
			screen.Update(tea.KeyPressMsg{Code: 'k'})
		}
		return screen.View()
	}(), "official:final-removal")
	if !strings.Contains(removal, "Remove") || !strings.Contains(removal, "High") {
		t.Fatalf("selected removal row hides its action/risk: %q", removal)
	}
	for screen.selected > 0 {
		screen.Update(tea.KeyPressMsg{Code: 'k'})
		visit(fmt.Sprintf("up to %d", screen.selected))
	}
}

func TestRestoreSkipsRemainReachableAfterOperationsAtCompactHeight(t *testing.T) {
	operations, skipped := longPlan()
	screen := exactRestore(operations, skipped)
	screen.selected = len(operations) - 1
	screen.Update(tea.KeyPressMsg{Code: 'j'})
	if screen.selected != len(operations) {
		t.Fatalf("j across the Changes → Skipped boundary selected %d, want %d", screen.selected, len(operations))
	}
	view := screen.View()
	assertWithinBudget(t, screen, view)
	if line := lineContaining(view, "skip-00"); !strings.Contains(line, "Safety") {
		t.Fatalf("first skip must be visible with its safety reason, got %q:\n%s", line, view)
	}
}

func TestRestoreUnboundedHeightKeepsFullLayout(t *testing.T) {
	screen := exactRestore(longPlan())
	screen.height = 0
	view := screen.View()
	for _, want := range []string{"Run settings", "Plan summary", "op-00", "official:final-removal", "skip-04"} {
		if !strings.Contains(view, want) {
			t.Fatalf("unbounded Restore view lost %q", want)
		}
	}
}
