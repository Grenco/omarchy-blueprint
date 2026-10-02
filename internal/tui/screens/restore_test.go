package screens

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type restoreErrorRunner struct{}

func (restoreErrorRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if name != "omarchy" || len(args) == 0 || args[0] != "version" {
		return "", errors.New("unexpected command")
	}
	if len(args) == 2 && args[1] == "channel" {
		return "stable", nil
	}
	return "4.0.0", nil
}

type countedRestoreProvider struct {
	restoreErrorProvider
	calls   atomic.Int32
	started chan struct{}
	block   bool
}

func (p *countedRestoreProvider) BindReadCycle(c *observation.Cycle) workflow.ReadProvider {
	slot := observation.NewSlot(c, func(ctx context.Context) (int, error) {
		p.calls.Add(1)
		if p.block {
			close(p.started)
			<-ctx.Done()
			return 0, ctx.Err()
		}
		return 1, nil
	}, func(v int) int { return v })
	return &countedRestoreRead{ReadProvider: workflow.NarrowReadProvider(p), slot: slot}
}

type countedRestoreRead struct {
	workflow.ReadProvider
	slot *observation.Slot[int]
}

func (p *countedRestoreRead) InspectTargets(ctx context.Context, _ profile.Data) ([]workflow.TargetInspection, error) {
	_, e := p.slot.Get(ctx)
	return nil, e
}
func (p *countedRestoreRead) Plan(ctx context.Context, _ profile.Data, _ omarchy.Info, _ workflow.RestoreContext) (workflow.RestoreFragment, error) {
	_, e := p.slot.Get(ctx)
	return workflow.RestoreFragment{Compatibility: model.CompatibilityCategory{Category: p.ID(), Authority: model.CompatibilityUnchanged}}, e
}

func readRestoreScreen(t *testing.T, p *countedRestoreProvider) *Restore {
	screen, _ := readRestoreScreenWithState(t, p)
	return screen
}

func readRestoreScreenWithState(t *testing.T, p *countedRestoreProvider) (*Restore, string) {
	t.Helper()
	dir, state := t.TempDir(), t.TempDir()
	if e := profile.Save(dir, profile.New("test", time.Unix(1, 0))); e != nil {
		t.Fatal(e)
	}
	session, e := workflow.Open(workflow.Dependencies{Runner: restoreErrorRunner{}, StateHome: func() (string, error) { return state, nil }}, workflow.Options{ProfileDir: dir})
	if e != nil {
		t.Fatal(e)
	}
	if e := session.SetProviders([]workflow.Provider{p}); e != nil {
		t.Fatal(e)
	}
	return NewRestore(session), state
}

func TestRestoreRefreshPresentsFreshMachineDefaultsWithoutSessionPublication(t *testing.T) {
	screen, state := readRestoreScreenWithState(t, &countedRestoreProvider{})
	session := screen.session
	d, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	d.Manifest.Capture.Hooks = true
	d.Machines.Items = []profile.Machine{{Name: "a"}, {Name: "b", RestoreConflicts: policy.ConflictForce, RestoreConvergence: policy.ConvergenceExact}}
	if err := profile.Save(session.ProfileDir(), d); err != nil {
		t.Fatal(err)
	}
	// Select A before opening the read; then externally switch to B.
	if err := session.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := session.UseMachine(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if err := (machine.BindingStore{StateHome: state}).Save(session.ProfileDir(), "b"); err != nil {
		t.Fatal(err)
	}
	screen.Update(screen.refreshPlan()())
	if screen.err != nil {
		t.Fatal(screen.err)
	}
	if screen.options.Conflicts != policy.ConflictForce || screen.options.Convergence != policy.ConvergenceExact {
		t.Fatal("preview options discarded", screen.options)
	}
	machineName, _, _, _, _, _, _ := screen.runSettings()
	if machineName != "b" {
		t.Fatal("fresh plan labelled with stale machine", machineName)
	}
	if confirmation := screen.confirmation(); !strings.Contains(confirmation, "Force conflicts, Exact convergence") {
		t.Fatal("confirmation lost fresh settings", confirmation)
	}
	if view := screen.View(); !strings.Contains(view, "Force") || !strings.Contains(view, "Exact") {
		t.Fatal("plan settings lost on render", view)
	}
	if session.Machine().Name != "a" {
		t.Fatal("read preview published Session state")
	}
}

func TestRestoreForceAndSafeRefreshShareOneObservation(t *testing.T) {
	p := &countedRestoreProvider{}
	screen := readRestoreScreen(t, p)
	screen.options = policy.DefaultRestoreOptions()
	screen.options.Conflicts = policy.ConflictForce
	screen.override = true
	message := screen.refreshPlan()().(restorePlanMsg)
	if message.err != nil || p.calls.Load() != 1 {
		t.Fatalf("refresh err=%v observations=%d", message.err, p.calls.Load())
	}
	if e := screen.refreshPlan()().(restorePlanMsg).err; e != nil {
		t.Fatal(e)
	}
	if p.calls.Load() != 2 {
		t.Fatal("new refresh reused old observation")
	}
}

func TestRestoreCanceledPreviewRejectsLateResult(t *testing.T) {
	p := &countedRestoreProvider{block: true, started: make(chan struct{})}
	screen := readRestoreScreen(t, p)
	ctx, cancel := context.WithCancel(context.Background())
	screen.ctx = ctx
	command := screen.refreshPlan()
	result := make(chan restorePlanMsg, 1)
	go func() { result <- command().(restorePlanMsg) }()
	<-p.started
	cancel()
	message := <-result
	if !errors.Is(message.err, context.Canceled) {
		t.Fatal(message.err)
	}
	screen.planRequestID++
	screen.current = model.RestorePlan{Skipped: []model.Skipped{{Reason: "newer result"}}}
	screen.Update(message)
	if screen.current.Skipped[0].Reason != "newer result" {
		t.Fatal("late canceled result replaced newer presentation")
	}
}

type restoreErrorProvider struct{}

func (restoreErrorProvider) ID() string                 { return "broken" }
func (restoreErrorProvider) Captured(profile.Data) bool { return true }
func (restoreErrorProvider) InspectTargets(context.Context, profile.Data) ([]workflow.TargetInspection, error) {
	return nil, nil
}
func (restoreErrorProvider) Capture(context.Context, *profile.Data, workflow.CaptureContext) (any, []model.Change, error) {
	return nil, nil, nil
}
func (restoreErrorProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return nil, nil
}
func (restoreErrorProvider) Plan(context.Context, profile.Data, omarchy.Info, workflow.RestoreContext) (workflow.RestoreFragment, error) {
	return workflow.RestoreFragment{}, errors.New("planning exploded")
}
func (restoreErrorProvider) Verify(context.Context, profile.Data, workflow.RestoreContext) (model.VerificationResult, error) {
	return model.VerificationResult{OK: true}, nil
}

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
	for _, want := range []string{"Run settings", "Conflict handling (f)", "Safe", "Keep conflicting files", "Convergence (e)", "Exact", "Remove managed extras", "One-run override", "WARNING: Exact", "Remove"} {
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

func TestRestoreCurrentPlanUsesResponsiveOperationAndSkipTables(t *testing.T) {
	for _, width := range []int{140, 100, 80} {
		screen := NewRestore(nil)
		screen.width = width
		screen.options = policy.RestoreOptions{Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceExact}
		screen.override = true
		screen.forcedOverrides = 1
		screen.current = model.RestorePlan{
			Operations: []model.Operation{{Provider: "packages", Resource: "official:spotify", Action: "remove", Command: []string{"pacman", "-R"}, Risk: model.RiskHigh}},
			Skipped:    []model.Skipped{{Provider: "config", Resource: ".config/example", Reason: "restore disabled by profile policy"}},
		}
		view := screen.View()
		for _, want := range []string{"Run settings", "Force", "Overwrite conflicting files", "Exact", "Remove managed extras", "Plan summary", "REMOVALS", "POLICY SKIPS", "FORCED OVERRIDES", "1", "Changes", "CATEGORY", "TARGET", "ACTION", "RISK", "Packages", "official:spotify", "Remove", "High", "Skipped by policy / safety", "REASON", "Config", ".config/example", "Policy: Skip"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%d-column Restore view missing %q:\n%s", width, want, view)
			}
		}
		if strings.Contains(view, "packages: official:spotify") || strings.Contains(view, "Skip: .config/example —") {
			t.Fatalf("%d-column Restore view retained prose rows:\n%s", width, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("%d-column Restore line is %d columns: %q", width, got, line)
			}
		}
	}
}

func TestRestoreCurrentPlanSeparatesSettingsSummaryAndTables(t *testing.T) {
	screen := NewRestore(nil)
	screen.width = 100
	screen.current = model.RestorePlan{
		Operations: []model.Operation{{Provider: "config", Resource: "settings", Action: "write", Risk: model.RiskLow}},
		Skipped:    []model.Skipped{{Provider: "packages", Resource: "extra", Reason: "additional package left installed; removal disabled"}},
	}
	view := screen.currentPlanView()
	lines := strings.Split(view, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	view = strings.Join(lines, "\n")
	for _, want := range []string{
		"Run settings ─",
		"Additive",
		"Keep additional items",
		"\n\nPlan summary ─",
		"FORCED OVERRIDES",
		"0\n\nChanges ─",
		"Low\n\nSkipped by policy / safety / mode ─",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("Restore summary lacks readable spacing %q:\n%s", want, view)
		}
	}
}

func TestRestoreSkipReasonLabelsDoNotInventSafety(t *testing.T) {
	for _, test := range []struct {
		reason string
		want   string
	}{
		{"restore disabled by profile policy", "Policy: Skip"},
		{"additional package left installed; removal disabled", "Additive"},
		{"overwrite disabled for existing file", "Safe"},
		{"hardware target blocked for this machine", "Safety"},
		{"already satisfied", "Skipped"},
	} {
		if got := skipReasonLabel(test.reason); got != test.want {
			t.Errorf("skipReasonLabel(%q) = %q, want %q", test.reason, got, test.want)
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
	for _, want := range []string{"Force conflicts", "Exact convergence", "2 removals", "1 high-risk", "2 forced override(s)"} {
		if !strings.Contains(confirmation, want) {
			t.Fatalf("confirmation missing %q: %s", want, confirmation)
		}
	}
	if strings.Contains(confirmation, "normal mode") {
		t.Fatalf("confirmation used legacy mode: %s", confirmation)
	}
}

func TestRestoreIgnoresOutOfOrderPlanResponses(t *testing.T) {
	screen := NewRestore(nil)
	screen.planRequestID = 2
	screen.Update(restorePlanMsg{requestID: 2, plan: model.RestorePlan{Operations: []model.Operation{{ID: "force-exact"}}}})
	screen.Update(restorePlanMsg{requestID: 1, plan: model.RestorePlan{Operations: []model.Operation{{ID: "safe-additive"}}}})
	if got := screen.current.Operations[0].ID; got != "force-exact" {
		t.Fatalf("stale plan replaced current authority: got %q", got)
	}
}

func TestRestoreCannotApplyWhileAReplacementPlanIsPending(t *testing.T) {
	session, _ := newSyncSession(t)
	screen := NewRestore(session)
	screen.current = model.RestorePlan{Operations: []model.Operation{{ID: "safe-additive"}}}
	if !screen.CanApply() {
		t.Fatal("seeded current plan should be applicable")
	}
	if cmd := screen.Update(tea.KeyPressMsg{Code: 'f'}); cmd == nil {
		t.Fatal("Force toggle did not request a replacement plan")
	}
	if !screen.planning || screen.CanApply() {
		t.Fatalf("replacement plan remains approvable: planning=%t canApply=%t", screen.planning, screen.CanApply())
	}
	if view := screen.View(); !strings.Contains(view, "Refreshing plan") {
		t.Fatalf("pending plan was not visibly marked:\n%s", view)
	}
	if cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || screen.confirm {
		t.Fatalf("pending plan opened confirmation: cmd=%v confirm=%t", cmd != nil, screen.confirm)
	}

	screen.Update(restorePlanMsg{requestID: screen.planRequestID, plan: model.RestorePlan{Operations: []model.Operation{{ID: "force-additive"}}}})
	if screen.planning || !screen.CanApply() {
		t.Fatalf("matching plan response did not restore apply authority: planning=%t canApply=%t", screen.planning, screen.CanApply())
	}
}

func TestRestorePlanningErrorsRemainVisible(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	if err := profile.Save(root, profile.New("test", time.Now())); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(workflow.Dependencies{Runner: restoreErrorRunner{}, StateHome: func() (string, error) { return state, nil }}, workflow.Options{ProfileDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetProviders([]workflow.Provider{restoreErrorProvider{}}); err != nil {
		t.Fatal(err)
	}
	screen := NewRestore(session)
	cmd := screen.Init()
	screen.Update(cmd())
	view := screen.View()
	if !strings.Contains(view, "Unable to prepare restore") || !strings.Contains(view, "planning exploded") {
		t.Fatalf("planning error was not visible:\n%s", view)
	}
	if strings.Contains(view, "No restore operations required") {
		t.Fatalf("planning failure was presented as convergence:\n%s", view)
	}
}

func TestRestoreCurrentPlanSelectionDrivesDetails(t *testing.T) {
	screen := NewRestore(nil)
	screen.current = model.RestorePlan{
		Operations: []model.Operation{{Provider: "config", Resource: "one", Action: "write", Risk: model.RiskLow}},
		Skipped:    []model.Skipped{{Provider: "packages", Resource: "two", Reason: "restore disabled"}},
	}
	if detail := screen.DetailView(); !strings.Contains(detail, "Restore operation") || !strings.Contains(detail, "Category: Config") || !strings.Contains(detail, "Target: one") {
		t.Fatalf("first current-plan detail = %q", detail)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if detail := screen.DetailView(); !strings.Contains(detail, "Restore skip") || !strings.Contains(detail, "Category: Packages") || !strings.Contains(detail, "Target: two") || !strings.Contains(detail, "restore disabled") {
		t.Fatalf("selected current-plan detail = %q", detail)
	}
}

func TestRestoreSanitizesCurrentPlanAndErrors(t *testing.T) {
	screen := NewRestore(nil)
	screen.current.Operations = []model.Operation{{Provider: "bad\nprovider\x1b", Resource: "bad\nresource\x1b"}}
	if view := screen.View(); strings.Contains(view, "\x1b") || !strings.Contains(view, "bad?resource?") {
		t.Fatalf("unsafe restore=%q", view)
	}
	if detail := screen.DetailView(); strings.Contains(detail, "\x1b") || !strings.Contains(detail, "Bad?provider?") {
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
