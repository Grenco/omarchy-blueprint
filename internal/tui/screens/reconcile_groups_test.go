package screens

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func groupState(fp, summary string) syncengine.State {
	return syncengine.State{Kind: syncengine.StatePresent, Fingerprint: fp, Summary: summary}
}

// groupedSyncPlan has every kind of target: a history conflict, a machine
// difference, incoming and outgoing changes, protected targets and many
// already matching ones.
func groupedSyncPlan(t *testing.T, matching int) syncengine.Plan {
	t.Helper()
	all := syncengine.EffectivePolicy{Capture: true, Restore: true}
	key := func(category, target string) syncengine.TargetKey {
		return syncengine.TargetKey{Category: category, Target: target}
	}
	targets := []syncengine.TargetInput{
		{Key: key("defaults", "browser"), Base: groupState("a", "chromium"), Local: groupState("b", "firefox"), Remote: groupState("c", "brave"), Machine: groupState("b", "firefox"), Policy: all},
		{Key: key("config", "bindings"), Base: groupState("a", "v1"), Local: groupState("a", "v1"), Remote: groupState("a", "v1"), Machine: groupState("m", "edited"), Policy: all},
		{Key: key("themes", "active"), Base: groupState("a", "tokyo"), Local: groupState("a", "tokyo"), Remote: groupState("c", "catppuccin"), Machine: groupState("a", "tokyo"), Policy: all},
		{Key: key("hooks", "post-update"), Base: syncengine.State{Kind: syncengine.StateUnmanaged}, Local: groupState("b", "3 lines"), Remote: syncengine.State{Kind: syncengine.StateUnmanaged}, Machine: groupState("b", "3 lines"), Policy: all},
		{Key: key("config", "protected"), Base: groupState("a", "saved"), Local: groupState("a", "saved"), Remote: groupState("a", "saved"), Machine: groupState("m", "edited"), Policy: syncengine.EffectivePolicy{}},
	}
	for i := 0; i < matching; i++ {
		targets = append(targets, syncengine.TargetInput{Key: key("packages", fmt.Sprintf("same-%03d", i)), Base: groupState("a", "installed"), Local: groupState("a", "installed"), Remote: groupState("a", "installed"), Machine: groupState("a", "installed"), Policy: all})
	}
	plan, err := syncengine.Reconcile(syncengine.PlanRequest{Identity: syncengine.Identity{Base: strings.Repeat("a", 40), Local: strings.Repeat("b", 40), Remote: strings.Repeat("c", 40), Machine: "desktop", Branch: "main", Upstream: "origin/main", RemoteFingerprint: strings.Repeat("d", 64), ControlFingerprint: strings.Repeat("e", 64)}, Git: syncengine.GitState{Clean: true}, Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func readySyncScreen(t *testing.T, plan syncengine.Plan, width, height int) (*Reconcile, *reconcileFixture) {
	t.Helper()
	f := &reconcileFixture{plan: plan}
	s := NewReconcileContext(context.Background(), f)
	s.SetSize(width, height)
	s.Update(s.Init()())
	return s, f
}

func headerIndex(t *testing.T, s *Reconcile, group string) int {
	t.Helper()
	for i, row := range s.rows() {
		if row.header && row.group == group {
			return i
		}
	}
	t.Fatalf("no %q heading in %v", group, s.rows())
	return -1
}

func TestReconcileGroupsCollapseIndependentlyWithAccurateCounts(t *testing.T) {
	s, _ := readySyncScreen(t, groupedSyncPlan(t, 84), 100, 30)
	want := map[string]int{reconcileNeedsDecision: 2, reconcileComputer: 1, reconcileSave: 1, reconcileProtected: 1, reconcileMatches: 84}
	defaults := map[string]bool{reconcileNeedsDecision: true, reconcileComputer: true, reconcileSave: true, reconcileProtected: false, reconcileMatches: false}
	for group, total := range want {
		if got := s.groupExpanded(group); got != defaults[group] {
			t.Fatalf("%s initially expanded=%t", group, got)
		}
		if !strings.Contains(s.headerLabel(group, total, true), fmt.Sprintf("%s (%d)", group, total)) {
			t.Fatalf("%s heading lacks its total", group)
		}
	}
	for group, total := range want {
		before := map[string]bool{}
		for other := range want {
			before[other] = s.groupExpanded(other)
		}
		s.list.SetSelected(headerIndex(t, s, group), len(s.rows()), s.listHeight())
		s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		for other := range want {
			expect := before[other]
			if other == group {
				expect = !expect
			}
			if s.groupExpanded(other) != expect {
				t.Fatalf("toggling %s changed %s", group, other)
			}
		}
		visible := 0
		for _, row := range s.rows() {
			if row.group == group && !row.header {
				visible++
			}
		}
		if (s.groupExpanded(group) && visible != total) || (!s.groupExpanded(group) && visible != 0) {
			t.Fatalf("%s shows %d of %d rows (expanded=%t)", group, visible, total, s.groupExpanded(group))
		}
		if row, _ := s.selectedRow(); !row.header || row.group != group {
			t.Fatal("selection left the toggled heading")
		}
	}
}

func TestReconcileInitialScreenIsAnOverview(t *testing.T) {
	s, _ := readySyncScreen(t, groupedSyncPlan(t, 300), 100, 30)
	view := s.View()
	for _, want := range []string{"Here's what Blueprint found", "2 decisions need your attention", "300 settings already match", "▶ Already matches (300)", "▶ Protected by your settings (1)", "▼ Needs your decision (2)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overview lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "same-000") {
		t.Fatal("already-matching settings listed individually on first display")
	}
	if row, ok := s.selectedRow(); !ok || row.item < 0 || reconcileGroup(s.current.Items[row.item]) != reconcileNeedsDecision {
		t.Fatal("first display did not select the first setting that needs a decision")
	}
}

func TestReconcileNavigationAndGroupingNeverChangeTheReview(t *testing.T) {
	s, f := readySyncScreen(t, groupedSyncPlan(t, 20), 100, 30)
	s.Update(tea.KeyPressMsg{Code: 'L'})
	plan, choices := s.current, fmt.Sprint(reconcileChoices(s.choices))
	for _, key := range []string{"]", "]", "enter", "space", "j", "j", "k", "[", "G", "g", "g", "enter", "down", "up", "]", "enter"} {
		var msg tea.KeyPressMsg
		switch key {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		default:
			msg = tea.KeyPressMsg{Code: rune(key[0]), Text: key}
		}
		if cmd := s.Update(msg); cmd != nil {
			t.Fatalf("%q returned a command", key)
		}
		if s.confirming || f.executions != 0 {
			t.Fatalf("%q started the Sync run flow", key)
		}
		row, ok := s.selectedRow()
		if !ok {
			t.Fatal("selection lost")
		}
		if !row.header && !s.groupExpanded(row.group) {
			t.Fatalf("%q selected a hidden setting", key)
		}
		detail := strings.SplitN(s.DetailView(), "\n", 2)[0]
		switch {
		case row.header && detail != row.group:
			t.Fatalf("Details %q does not match heading %q", detail, row.group)
		case row.item >= 0 && detail != reconcileTargetName(s.current.Items[row.item].Key):
			t.Fatalf("Details %q does not match row %v", detail, s.current.Items[row.item].Key)
		}
	}
	if !reflect.DeepEqual(plan, s.current) || choices != fmt.Sprint(reconcileChoices(s.choices)) {
		t.Fatal("navigation or grouping changed decisions or the plan")
	}
}

func TestReconcileRunNeedsTheReviewKeyAndExplicitConfirmation(t *testing.T) {
	s, f := readySyncScreen(t, groupedSyncPlan(t, 3), 100, 30)
	decide := func(target string, key rune) {
		for i, row := range s.rows() {
			if row.item >= 0 && s.current.Items[row.item].Key.Target == target {
				s.list.SetSelected(i, len(s.rows()), s.listHeight())
				s.Update(tea.KeyPressMsg{Code: key})
				return
			}
		}
		t.Fatalf("no row for %s", target)
	}
	decide("browser", 'L')
	decide("bindings", 'p')
	if !s.CanExecute() {
		t.Fatal("fixture is not ready to run")
	}
	for range s.rows() {
		if cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || s.confirming {
			t.Fatal("Enter started Sync")
		}
		s.Update(tea.KeyPressMsg{Code: 'j'})
	}
	s.Update(tea.KeyPressMsg{Code: 's'})
	if !s.confirming || f.executions != 0 || !strings.Contains(s.View(), "Review your Sync changes") {
		t.Fatal("s did not open the run review, or ran early")
	}
	s.Update(tea.KeyPressMsg{Code: 'b'})
	if s.confirming || !s.CanExecute() {
		t.Fatal("Back to review lost the review")
	}
	s.Update(tea.KeyPressMsg{Code: 's'})
	s.Update(tea.KeyPressMsg{Code: 'c'})
	if s.confirming || len(s.choices) != 0 || s.CanExecute() || f.executions != 0 || s.message != reconcileCancelledRun {
		t.Fatal("Cancel did not stop without running")
	}
}

func setupScreen(t *testing.T, plan workflow.SyncBootstrapPlan, width, height int) *Reconcile {
	t.Helper()
	f := &reconcileFixture{plan: syncengine.Plan{BootstrapRequired: true}, bootstrap: plan}
	s := NewReconcileContext(context.Background(), f)
	s.SetSize(width, height)
	s.Update(s.Init()())
	return s
}

func baseSetupPlan() workflow.SyncBootstrapPlan {
	return workflow.SyncBootstrapPlan{Identity: syncengine.Identity{Local: strings.Repeat("b", 40), Remote: strings.Repeat("b", 40), Machine: "laptop", Branch: "main", Upstream: "origin/main"}, Git: syncengine.GitState{Clean: true}}
}

func TestReconcileFirstTimeSetupExplainsBothChoices(t *testing.T) {
	s := setupScreen(t, baseSetupPlan(), 100, 60)
	text := strings.Join(strings.Fields(s.View()), " ")
	for _, want := range []string{
		"Set up Sync on this computer",
		"Blueprint has not completed a Sync on this computer yet.",
		"Start with my saved Blueprint setup", "Use the setup already saved in Git as the starting point.", "See what would change",
		"Start with this computer's setup", "Blueprint will respect settings you have chosen not to save.", "See what would be saved",
		"Cancel setup — leave everything as it is",
		"Your saved profile and the shared profile already match.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("setup lacks %q:\n%s", want, s.View())
		}
	}
	if strings.Contains(strings.ToLower(text), "bootstrap") {
		t.Fatal("setup uses the word bootstrap")
	}
	s.Update(tea.KeyPressMsg{Code: 'v'})
	if s.bootstrapChoice == nil || *s.bootstrapChoice != "adopt-machine" || s.confirming {
		t.Fatal("preview did not open, or went further than a preview")
	}
}

func TestReconcileBlockedStartFromThisComputerNamesProtectedSettings(t *testing.T) {
	plan := baseSetupPlan()
	key := func(category, target string) syncengine.TargetKey {
		return syncengine.TargetKey{Category: category, Target: target}
	}
	plan.Items = []workflow.SyncBootstrapItem{
		{Key: key("themes", "theme:aether"), Local: groupState("x", "empty"), Remote: groupState("x", "empty"), Machine: groupState("y", "folder"), Policy: syncengine.EffectivePolicy{Restore: true}},
		{Key: key("config", "app.json"), Local: groupState("x", "saved"), Remote: groupState("x", "saved"), Machine: groupState("y", "edited"), Inapplicable: true},
		{Key: key("defaults", "browser"), Local: groupState("x", "firefox"), Remote: groupState("x", "firefox"), Machine: groupState("x", "firefox"), Policy: syncengine.EffectivePolicy{Capture: true, Restore: true}, Converged: true},
	}
	s := setupScreen(t, plan, 100, 60)
	view := strings.Join(strings.Fields(s.View()), " ")
	for _, want := range []string{"Can't start from this computer yet", "Blueprint is currently set not to save those settings", "Nothing has been changed.", "2 settings are protected from Capture", "Show protected settings", "Review Capture settings", "Choose the saved setup instead"} {
		if !strings.Contains(view, want) {
			t.Fatalf("blocked choice lacks %q:\n%s", want, s.View())
		}
	}
	if strings.Contains(view, "lineage") || strings.Contains(view, "CLI") {
		t.Fatal("blocked choice uses the old CLI/lineage advice")
	}
	actions := map[string]SyncAction{}
	for _, action := range s.Actions() {
		actions[action.ID] = action
	}
	if actions["reconcile.bootstrap-machine"].Enabled || !actions["reconcile.bootstrap-upstream"].Enabled || !actions["reconcile.show-protected"].Enabled {
		t.Fatal("preflight did not disable only the refused choice")
	}
	s.Update(tea.KeyPressMsg{Code: 'v'})
	if s.bootstrapChoice != nil {
		t.Fatal("refused choice opened")
	}
	listed := map[string]bool{}
	for _, row := range s.rows() {
		if row.setup != nil {
			listed[row.setup.Key.Target] = true
		}
	}
	if !listed["theme:aether"] || !listed["app.json"] || listed["browser"] {
		t.Fatalf("protected list = %v", listed)
	}
	detail := s.DetailView()
	for _, want := range []string{"Capture allowed: No", "Restore allowed: Yes", "Why this matters", "Press c to open the Themes Capture settings", "will not change this permission"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("protected setting detail lacks %q:\n%s", want, detail)
		}
	}
	cmd := s.Update(tea.KeyPressMsg{Code: 'c'})
	if cmd == nil {
		t.Fatal("Review Capture settings did nothing")
	}
	if nav, ok := cmd().(PolicyNavigation); !ok || nav.Category != "themes" {
		t.Fatalf("Review Capture settings opened %#v", cmd())
	}
}

func TestReconcileOtherBlockersExplainTheirActualCause(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*workflow.SyncBootstrapPlan)
		wants []string
	}{
		{"uncommitted", func(p *workflow.SyncBootstrapPlan) { p.Git.Clean = false }, []string{"Your saved profile has changes that have not been committed.", "Finish saving or discard those changes before starting Sync."}},
		{"diverged", func(p *workflow.SyncBootstrapPlan) { p.Git.Diverged, p.Git.Ahead, p.Git.Behind = true, 1, 1 }, []string{"Blueprint cannot safely match the history on this computer with the shared profile. Nothing has been changed.", "Git history"}},
		{"unreachable", func(p *workflow.SyncBootstrapPlan) {
			p.Blockers = []syncengine.Blocker{{Code: "remote-unavailable", Message: "Configured Sync upstream could not be fetched"}}
		}, []string{"Can't reach the shared profile"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := baseSetupPlan()
			tc.edit(&plan)
			s := setupScreen(t, plan, 100, 60)
			text := strings.Join(strings.Fields(s.View()+"\n"+s.DetailView()), " ")
			for _, want := range tc.wants {
				if !strings.Contains(text, want) {
					t.Fatalf("lacks %q:\n%s", want, text)
				}
			}
			if strings.Contains(text, "lineage") || strings.Contains(text, "Capture settings") || strings.Contains(text, "protected from Capture") {
				t.Fatalf("unrelated advice shown:\n%s", text)
			}
			for _, action := range s.Actions() {
				if (action.ID == "reconcile.bootstrap-upstream" || action.ID == "reconcile.bootstrap-machine") && action.Enabled {
					t.Fatal("blocked choice looks available")
				}
			}
		})
	}
}

func TestReconcileLongListsFitEveryWorkspace(t *testing.T) {
	styles := components.NewStyles(components.ThemePalette{ColorEnabled: true, Accent: "#89b4fa", Muted: "#6c7086", Warning: "#f9e2af", Success: "#a6e3a1", Selection: "#313244", Foreground: "#cdd6f4"})
	for _, size := range append([][2]int{{138, 31}, {98, 21}, {78, 15}, {40, 12}, {60, 6}}, [][2]int{{120, 40}}...) {
		s, _ := readySyncScreen(t, groupedSyncPlan(t, 300), size[0], size[1])
		s.SetStyles(styles)
		s.expanded[reconcileMatches] = true
		for i := 0; i < 120; i++ {
			s.Update(tea.KeyPressMsg{Code: 'j'})
			view := s.View()
			assertFits(t, fmt.Sprintf("Sync %dx%d step %d", size[0], size[1], i), view, size[0], size[1])
			if size[1] >= 15 && !strings.Contains(view, "> ") && !strings.Contains(view, "\x1b[") {
				t.Fatal("selection not rendered")
			}
		}
		s.Update(tea.KeyPressMsg{Code: 's'})
		assertFits(t, "Sync confirmation", s.View(), size[0], size[1])
	}
}
