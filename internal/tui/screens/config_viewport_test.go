package screens

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	config "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func manyConfigCandidates(n int) []config.Candidate {
	candidates := make([]config.Candidate, n)
	for i := range candidates {
		// Short paths, so narrow tables never truncate the part tests look for.
		candidates[i] = config.Candidate{Path: fmt.Sprintf(".c/app-%03d", i), Classification: config.ConfigAdded}
	}
	return candidates
}

func manyPolicyTargets(n int) ([]workflow.TargetInspection, map[string]policy.Effective) {
	targets := make([]workflow.TargetInspection, n)
	effective := map[string]policy.Effective{}
	for i := range targets {
		key := fmt.Sprintf(".config/app-%03d", i)
		targets[i] = workflow.TargetInspection{Key: key, Parent: ".config", CaptureEligible: true, RestoreEligible: true}
		effective[key] = policy.Effective{}
	}
	return targets, effective
}

func TestConfigStateViewFitsWithEveryStatusLineAndKeepsTheSelectionVisible(t *testing.T) {
	for _, size := range workspaceSizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			screen := &Config{width: size.width, height: size.height, candidates: manyConfigCandidates(60)}
			for range 60 {
				screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			}
			screen.err = errors.New("rescan failed: " + strings.Repeat("a very long reason ", 8))
			screen.notice, screen.busy = "Policy updated.", false
			screen.filtering, screen.filter = true, ""
			view := screen.View()
			assertFits(t, "Config State", view, size.width, size.height)
			if !strings.Contains(view, "app-059") {
				t.Fatalf("the selected last row scrolled out of view:\n%s", view)
			}
		})
	}
}

func TestConfigPolicyViewFitsAndKeepsTheSelectionVisible(t *testing.T) {
	for _, size := range workspaceSizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			targets, effective := manyPolicyTargets(50)
			screen := &Config{width: size.width, height: size.height, tab: "Capture", targets: targets, effective: effective}
			for range 50 {
				screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			}
			view := screen.View()
			assertFits(t, "Config policy", view, size.width, size.height)
			if !strings.Contains(view, "app-049") {
				t.Fatalf("the selected last policy row scrolled out of view:\n%s", view)
			}
		})
	}
}

func TestConfigDiffFitsBeneathItsTitle(t *testing.T) {
	document := longDiff(200)
	document.OldLabel = "baseline/" + strings.Repeat("deep/", 30) + "file"
	for _, size := range workspaceSizes {
		screen := &Config{width: size.width, height: size.height, candidates: manyConfigCandidates(1)}
		screen.inspection = workflow.ConfigInspection{BaselineToLive: &document}
		screen.diff = ptrDiff(document, screen.width, screen.diffHeight(), screen.styles)
		assertFits(t, "Config diff", screen.View(), size.width, size.height)
		screen.SetSize(size.width, size.height) // resizing an open diff keeps it within bounds
		assertFits(t, "Config diff after resize", screen.View(), size.width, size.height)
	}
}

// A rescan can shorten the policy list while the selection still points at
// a row that no longer exists; acting on it used to index past the end.
func TestConfigPolicyKeysAfterARescanShortensTheList(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: ' ', Text: " "}, {Code: 'x', Text: "x"}} {
		t.Run(key.String(), func(t *testing.T) {
			targets, effective := manyPolicyTargets(10)
			screen := &Config{width: 100, height: 20, tab: "Capture", targets: targets, effective: effective}
			for range 10 {
				screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			}
			shorter, shorterEffective := manyPolicyTargets(3)
			screen.statusID = 7
			screen.Update(configStatusMsg{requestID: 7, targets: shorter, effective: shorterEffective})
			screen.busy = false
			screen.Update(key) // must not panic
			if screen.policySelected < 0 || screen.policySelected >= 3 {
				t.Fatalf("selection %d is outside the 3 remaining rows", screen.policySelected)
			}
		})
	}
}

func TestConfigPolicySelectionFollowsItsTargetAcrossARescan(t *testing.T) {
	targets, effective := manyPolicyTargets(10)
	screen := &Config{width: 100, height: 20, tab: "Capture", targets: targets, effective: effective}
	for range 6 {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if screen.policySelectedKey != ".config/app-006" {
		t.Fatalf("selected %q", screen.policySelectedKey)
	}
	// A rescan that drops earlier rows keeps the same target selected.
	screen.statusID = 3
	screen.Update(configStatusMsg{requestID: 3, targets: targets[4:], effective: effective})
	screen.View()
	if rows := screen.configPolicyRows(); rows[screen.policySelected].target.Key != ".config/app-006" {
		t.Fatalf("selection moved to %q after the rescan", rows[screen.policySelected].target.Key)
	}
}

// longDiff is a text diff with n alternating removed and added lines.
func longDiff(n int) inspection.DiffDocument {
	var lines []inspection.DiffLine
	for i := range n {
		kind := "add"
		if i%2 == 0 {
			kind = "remove"
		}
		lines = append(lines, inspection.DiffLine{Kind: kind, OldLine: i + 1, NewLine: i + 1, Text: fmt.Sprintf("line %d", i)})
	}
	return inspection.DiffDocument{Kind: inspection.DiffText, OldLabel: "baseline", NewLabel: "live", Hunks: []inspection.DiffHunk{{OldStart: 1, OldCount: n, NewStart: 1, NewCount: n, Lines: lines}}}
}
