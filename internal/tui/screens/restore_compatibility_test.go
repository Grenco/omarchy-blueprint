package screens

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

func compatibilityReviewPlan() model.RestorePlan {
	return model.RestorePlan{
		Compatibility: model.CompatibilityReport{
			ProfileLastCapture: model.CompatibilityEnvironment{},
			Target:             model.CompatibilityEnvironment{Known: true, OmarchyVersion: "4.2.0", OmarchyChannel: "stable"},
			Categories: []model.CompatibilityCategory{
				{Category: "config", Applies: false, Authority: model.CompatibilityUnchanged},
				{Category: "hooks", Applies: true, State: model.CompatibilityUnknown, Authority: model.CompatibilityReduced, Findings: []model.CompatibilityFinding{{Code: "hooks.lifecycle.unknown", Target: "startup", State: model.CompatibilityUnknown, Authority: model.CompatibilityReduced, Summary: "lifecycle not established"}}},
				{Category: "plugins", Applies: true, State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Findings: []model.CompatibilityFinding{{Code: "plugins.first_party.unavailable", Target: "plugin:clock", State: model.CompatibilityIncompatible, Authority: model.CompatibilityBlocked, Summary: "first-party plugin unavailable"}}},
				{Category: "resources", Applies: true, State: model.CompatibilitySupported, Authority: model.CompatibilityUnchanged, Evidence: []model.CompatibilityEvidence{{Kind: "portable", Summary: "portable copy"}}},
			},
		},
		Operations: []model.Operation{{Provider: "resources", Resource: "resource:notes", Action: "copy", Risk: model.RiskLow}},
	}
}

// Pinned by tui.TestRestoreWorkspaceBudgetAt70x18MatchesScreenTests: at the
// application's minimum 70x18 frame, Restore receives this exact interior.
const restoreMinimumWidth, restoreMinimumHeight = 68, 9

func TestRestoreCompatibilityBlockerVisibleAtMinimumApplicationSize(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = restoreMinimumWidth, restoreMinimumHeight
	screen.current = compatibilityReviewPlan()
	view := screen.View()
	assertWithinBudget(t, screen, view)
	for _, want := range []string{"Compatibility", "Installed plugins", "Blocked", "plugin:clock", "first-party plugin unavailable", "resource:notes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("minimum-size Restore hid blocker detail %q:\n%s", want, view)
		}
	}
}

func TestRestoreCompatibilityMinimumSizePrioritizesBlockerWithExactAndReadiness(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = restoreMinimumWidth, restoreMinimumHeight
	screen.current = compatibilityReviewPlan()
	screen.options.Convergence = policy.ConvergenceExact
	screen.override = true
	screen.current.Requirements = []model.Requirement{{ID: "packages.metadata", Reason: "package metadata unavailable", Remediation: []string{"omarchy", "update"}}}
	screen.current.Compatibility.Categories[2].Findings = append([]model.CompatibilityFinding{{Code: "plugins.provenance.unknown", Target: "plugin:other", State: model.CompatibilityUnknown, Authority: model.CompatibilityUnchanged, Summary: "source not established"}}, screen.current.Compatibility.Categories[2].Findings...)
	view := screen.View()
	assertWithinBudget(t, screen, view)
	for _, want := range []string{"Compatibility", "Installed plugins", "plugin:clock", "first-party plugin unavailable", "Exact", "omarchy update", "resource:notes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("minimum-size Exact/readiness review hid %q:\n%s", want, view)
		}
	}
}

func TestRestoreCompatibilityRendersSamePlanAtWideAndCompactSizes(t *testing.T) {
	for _, size := range []struct{ width, height int }{{140, 40}, {100, 32}, {restoreCompactWidth, restoreCompactHeight}} {
		screen := NewRestore(nil)
		screen.width, screen.height = size.width, size.height
		screen.current = compatibilityReviewPlan()
		screen.compatibilityExpanded = true
		view := screen.View()
		assertWithinBudget(t, screen, view)
		for _, want := range []string{"Compatibility", "Profile last capture: unknown", "Target Omarchy: 4.2.0", "Installed plugins", "Incompatible · Blocked", "plugin:clock", "first-party plugin unavailable", "Hooks", "Unknown · Reduced", "lifecycle not established", "Resources", "Supported · Unchanged", "Config", "Not selected", "resource:notes"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%dx%d compatibility view missing %q:\n%s", size.width, size.height, want, view)
			}
		}
		if strings.Index(view, "Compatibility") > strings.Index(view, "Changes") || strings.Index(view, "Incompatible · Blocked") > strings.Index(view, "first-party plugin unavailable") {
			t.Fatalf("compatibility or finding shown after work:\n%s", view)
		}
	}
}

func TestRestoreCompatibilityDefaultCondensesInformationalUncertainty(t *testing.T) {
	screen := NewRestore(nil)
	screen.SetSize(78, 16)
	screen.current = compatibilityReviewPlan()
	screen.current.Compatibility.Categories[2].Applies = false
	for i := range screen.current.Compatibility.Categories {
		screen.current.Compatibility.Categories[i].Authority = model.CompatibilityUnchanged
	}
	view := screen.View()
	assertWithinBudget(t, screen, view)
	if !strings.Contains(view, "Compatibility: Ready") || strings.Contains(view, "lifecycle not established") || strings.Contains(view, "Profile last capture") || !strings.Contains(view, "resource:notes") {
		t.Fatalf("default compatibility swamped operation table:\n%s", view)
	}
	screen.Update(tea.KeyPressMsg{Code: 'v'})
	if details := screen.DetailView(); !strings.Contains(details, "lifecycle not established") || !strings.Contains(details, "Profile last capture") {
		t.Fatalf("expanded report lost evidence: %s", details)
	}
	screen.Update(tea.KeyPressMsg{Code: 'v'})
	if screen.compatibilityExpanded {
		t.Fatal("compatibility did not collapse")
	}
}

func TestRestoreCompatibilityReducedSummarizesWithheldAuthority(t *testing.T) {
	screen := NewRestore(nil)
	screen.SetSize(78, 16)
	screen.current = compatibilityReviewPlan()
	screen.current.Compatibility.Categories[2].Applies = false
	view := screen.View()
	assertWithinBudget(t, screen, view)
	if !strings.Contains(view, "1 limited") || !strings.Contains(view, "some operations withheld") || strings.Contains(view, "lifecycle not established") {
		t.Fatalf("reduced compatibility did not summarize:\n%s", view)
	}
}

func TestRestoreCompatibilityBlocksConfirmationWithoutSuppressingReducedWork(t *testing.T) {
	screen := NewRestore(nil)
	screen.current = compatibilityReviewPlan()
	if screen.CanApply() {
		t.Fatal("blocked compatibility still enabled Apply")
	}
	if cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || screen.confirm {
		t.Fatalf("blocked compatibility opened modal: cmd=%t confirm=%t", cmd != nil, screen.confirm)
	}
	screen.options.Conflicts = policy.ConflictForce
	if screen.CanApply() {
		t.Fatal("Force bypassed blocked compatibility")
	}
	screen.current.Compatibility.Categories[2].Applies = false
	screen.current.Compatibility.Categories[2].Findings = nil
	screen.current.Compatibility.Categories[2].State = ""
	screen.current.Compatibility.Categories[2].Authority = model.CompatibilityUnchanged
	if !screen.CanApply() {
		t.Fatal("Unknown + Reduced alone incorrectly blocked safe operations")
	}
	if cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil || !screen.confirm {
		t.Fatal("non-blocking plan did not open confirmation")
	}
}

func TestRestoreApplyDisabledReasonMatchesSharedApplicabilityAndScreenState(t *testing.T) {
	blocked := compatibilityReviewPlan()
	ready := blocked
	ready.Compatibility = model.CompatibilityReport{}
	requiring := ready
	requiring.Requirements = []model.Requirement{{ID: "packages.metadata", Reason: "package metadata unavailable", Remediation: []string{"omarchy", "update"}}}
	for _, tc := range []struct {
		name, want string
		plan       model.RestorePlan
		planning   bool
		busy       bool
		err        error
	}{
		{name: "blocked-with-operations", plan: blocked, want: "aren't ready"},
		{name: "unmet-requirements", plan: requiring, want: "requirements must be completed"},
		{name: "no-operations", plan: model.RestorePlan{}, want: "no operations"},
		{name: "refreshing", plan: ready, planning: true, want: "plan is refreshing"},
		{name: "applying", plan: ready, busy: true, want: "restore is applying"},
		{name: "planning-error", plan: ready, err: errors.New("bad plan"), want: "could not be prepared"},
		{name: "applicable", plan: ready, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			screen := NewRestore(nil)
			screen.current, screen.planning, screen.busy, screen.err = tc.plan, tc.planning, tc.busy, tc.err
			reason := screen.ApplyDisabledReason()
			if !strings.Contains(reason, tc.want) || screen.CanApply() != (reason == "") {
				t.Fatalf("reason=%q canApply=%t, want %q", reason, screen.CanApply(), tc.want)
			}
		})
	}
}

func TestRestoreCompatibilityAndRequirementRemainVisibleAtCompactHeight(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = restoreCompactWidth, restoreCompactHeight
	screen.current = compatibilityReviewPlan()
	screen.current.Requirements = []model.Requirement{{ID: "packages.metadata", Reason: "package metadata unavailable", Remediation: []string{"omarchy", "update"}}}
	view := screen.View()
	assertWithinBudget(t, screen, view)
	for _, want := range []string{"Compatibility", "Incompatible · Blocked", "first-party plugin unavailable", "Requires before applying", "omarchy update", "resource:notes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("compact review hid %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "Compatibility") > strings.Index(view, "Requires before applying") {
		t.Fatalf("compact header changed compatibility/readiness order:\n%s", view)
	}
}

func TestRestoreCompatibilitySurvivesManyRequirementsAtCompactHeight(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = restoreCompactWidth, restoreCompactHeight
	screen.current = compatibilityReviewPlan()
	for i := range 8 {
		screen.current.Requirements = append(screen.current.Requirements, model.Requirement{ID: fmt.Sprintf("readiness-%d", i), Reason: "external readiness still needs attention", Remediation: []string{"omarchy", "update"}})
	}
	view := screen.View()
	assertWithinBudget(t, screen, view)
	for _, want := range []string{"Compatibility", "Incompatible · Blocked", "first-party plugin unavailable", "Requires before applying", "omarchy update", "more requirements", "resource:notes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("many requirements hid %q:\n%s", want, view)
		}
	}
}

func TestRestoreCompatibilityLongReportKeepsSelectedSkipAndBlockerVisible(t *testing.T) {
	operations, skipped := longPlan()
	screen := exactRestore(operations, skipped)
	screen.current.Compatibility = compatibilityReviewPlan().Compatibility
	for _, id := range []string{"packages", "themes", "defaults", "shell"} {
		screen.current.Compatibility.Categories = append(screen.current.Compatibility.Categories, model.CompatibilityCategory{Category: id, Applies: true, State: model.CompatibilitySupported, Authority: model.CompatibilityUnchanged})
	}
	screen.selected = len(operations)
	view := screen.View()
	assertWithinBudget(t, screen, view)
	for _, want := range []string{"Compatibility", "Blocked", "plugin:clock", "skip-00", "Skipped", "Exact"} {
		if !strings.Contains(view, want) {
			t.Fatalf("long compatibility header hid %q:\n%s", want, view)
		}
	}
}

func TestRestoreCompatibilitySanitizesFindingAndDoesNotClaimBlockedConvergence(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = restoreCompactWidth, restoreCompactHeight
	screen.current = compatibilityReviewPlan()
	screen.current.Operations = nil
	screen.current.Compatibility.Categories[2].Findings[0].Summary = "bad\nsummary\x1b"
	view := screen.View()
	assertWithinBudget(t, screen, view)
	if strings.Contains(view, "\x1b") || !strings.Contains(view, "bad?summary?") || strings.Contains(view, "No restore operations required") {
		t.Fatalf("unsafe or misleading blocked review:\n%s", view)
	}
}

func TestRestoreRequirementWithoutOperationsDoesNotClaimConvergence(t *testing.T) {
	screen := NewRestore(nil)
	screen.width, screen.height = restoreCompactWidth, restoreCompactHeight
	screen.current.Requirements = []model.Requirement{{ID: "packages.metadata", Reason: "package metadata unavailable", Remediation: []string{"omarchy", "update"}}}
	view := screen.View()
	assertWithinBudget(t, screen, view)
	if !strings.Contains(view, "Restore can't apply yet") || strings.Contains(view, "No restore operations required") {
		t.Fatalf("unmet readiness presented as convergence:\n%s", view)
	}
}
