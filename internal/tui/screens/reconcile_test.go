package screens

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

type reconcileFixture struct {
	bootstrap         workflow.SyncBootstrapPlan
	plan              syncengine.Plan
	calls, executions int
	fetch             bool
	err               error
	execute           func(context.Context, workflow.SyncExecutionRequest) (workflow.SyncExecutionResult, error)
}

type failingSyncWriter struct{ writes int }

func (w *failingSyncWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		return len(p), nil
	}
	return 0, errors.New("broken terminal")
}

func TestReconcileCannotApproveAnUnseenFreshReview(t *testing.T) {
	f := &reconcileFixture{execute: func(ctx context.Context, req workflow.SyncExecutionRequest) (workflow.SyncExecutionResult, error) {
		if err := req.ApproveCapture(ctx, workflow.CaptureInspection{}); err == nil {
			t.Fatal("accepted approval despite failing review output")
		}
		return workflow.SyncExecutionResult{}, errors.New("stopped")
	}}
	e := &reconcileExec{ctx: context.Background(), session: f, in: strings.NewReader("yes\n"), out: &failingSyncWriter{}}
	e.Run()
	if e.err == nil {
		t.Fatal("missing stopped execution")
	}
}

func TestReconcileGroupsPresentationWithoutChangingAuthorityOrder(t *testing.T) {
	s := NewReconcileContext(context.Background(), nil)
	s.ready = true
	s.current.Items = []syncengine.Item{
		{TargetInput: syncengine.TargetInput{Key: syncengine.TargetKey{Category: "defaults", Target: "a"}}, Review: syncengine.ReviewNone},
		{TargetInput: syncengine.TargetInput{Key: syncengine.TargetKey{Category: "defaults", Target: "b"}}, History: syncengine.HistoryRemote, Review: syncengine.ReviewNone},
		{TargetInput: syncengine.TargetInput{Key: syncengine.TargetKey{Category: "defaults", Target: "c"}}, Review: syncengine.ReviewMachine},
	}
	s.SetSize(100, 30)
	s.selectAfterInspection(reconcileRowKey{}, false)
	for _, group := range []string{reconcileNeedsDecision, reconcileNothing} {
		s.expanded[group] = true
	}
	view := s.View()
	if strings.Index(view, "Defaults · c") > strings.Index(view, "Defaults · a") || strings.Index(view, "Needs your decision") > strings.Index(view, "Nothing to change") {
		t.Fatal("target groups are not contiguous in review priority order")
	}
	if s.current.Items[0].Key.Target != "a" || !strings.Contains(s.DetailView(), "Defaults · c") {
		t.Fatal("presentation mutated canonical authority order or selected wrong target")
	}
}

func TestReconcileControlOnlyAndBlockedPlansRemainReviewable(t *testing.T) {
	s := NewReconcileContext(context.Background(), nil)
	s.ready = true
	s.current.Control = &syncengine.ControlChange{History: syncengine.HistoryRemote, SelectedSource: syncengine.ControlSourceRemote}
	s.current.Blockers = []syncengine.Blocker{{Code: "control-reinspection", Message: "Observation scope changed; inspect selected control inputs again."}}
	s.selectAfterInspection(reconcileRowKey{}, false)
	detail := s.DetailView()
	if !strings.Contains(detail, "rules for which config files") || !strings.Contains(detail, "Observation scope changed") || !strings.Contains(strings.Join(s.overviewDetail(), "\n"), "Control settings: remote, using remote") {
		t.Fatal("control-only plan lost its separate classification/blocker details")
	}
	if s.CanExecute() {
		t.Fatal("blocked plan became executable")
	}
}

type cancelReconcileFixture struct {
	reconcileFixture
	started chan struct{}
}

func (f *cancelReconcileFixture) InspectSync(ctx context.Context, _ workflow.SyncInspectOptions) (syncengine.Plan, error) {
	close(f.started)
	<-ctx.Done()
	return f.plan, ctx.Err()
}
func TestReconcileFetchCancellationJoinsAndRejectsFutureResult(t *testing.T) {
	f := &cancelReconcileFixture{reconcileFixture: reconcileFixture{plan: conflictSyncPlan(t)}, started: make(chan struct{})}
	s := NewReconcileContext(context.Background(), f)
	cmd := s.Init()
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	<-f.started
	s.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	select {
	case msg := <-result:
		s.Update(msg)
	case <-time.After(time.Second):
		t.Fatal("cancelled inspection did not join")
	}
	if s.CanExecute() || s.ready {
		t.Fatal("cancelled result became an executable plan")
	}
}

func (f *reconcileFixture) InspectSyncBootstrap(context.Context, workflow.SyncInspectOptions) (workflow.SyncBootstrapPlan, error) {
	return f.bootstrap, nil
}
func TestReconcileBootstrapRequiresExplicitChoice(t *testing.T) {
	f := &reconcileFixture{plan: syncengine.Plan{BootstrapRequired: true}, bootstrap: workflow.SyncBootstrapPlan{Identity: syncengine.Identity{Local: strings.Repeat("b", 40), Remote: strings.Repeat("b", 40), Machine: "desktop", Branch: "main", Upstream: "origin/main", RemoteFingerprint: strings.Repeat("d", 64), ControlFingerprint: strings.Repeat("e", 64)}, Git: syncengine.GitState{Clean: true}, Choices: []string{"reconcile-upstream", "adopt-machine", "cancel"}, ConvergenceEligible: true}}
	s := NewReconcileContext(context.Background(), f)
	s.Update(s.Init()())
	if s.CanExecute() {
		t.Fatal("missing cursor implicitly authorized bootstrap")
	}
	s.Update(tea.KeyPressMsg{Code: 'u'})
	if !s.CanExecute() || !strings.Contains(s.View(), "First-time Sync setup: Start with my saved Blueprint setup") {
		t.Fatal("explicit reviewed bootstrap choice unavailable")
	}
	s.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if s.CanExecute() || f.executions != 0 {
		t.Fatal("cancel did not discard baseless choice without mutation")
	}
}

func (f *reconcileFixture) InspectSync(ctx context.Context, opt workflow.SyncInspectOptions) (syncengine.Plan, error) {
	f.calls++
	f.fetch = opt.Fetch
	if err := ctx.Err(); err != nil {
		return syncengine.Plan{}, err
	}
	return f.plan, f.err
}
func (f *reconcileFixture) ExecuteSync(ctx context.Context, req workflow.SyncExecutionRequest) (workflow.SyncExecutionResult, error) {
	f.executions++
	if f.execute != nil {
		return f.execute(ctx, req)
	}
	return workflow.SyncExecutionResult{}, f.err
}

func TestReconcileExecutionRequiresFreshAuthorityReviewsAndRefreshesAfterFailure(t *testing.T) {
	for _, cause := range []error{errors.New("stale plan /private/path"), errors.New("busy lock"), errors.New("push rejection"), errors.New("Restore failed"), errors.New("cursor save failed"), profilegit.ErrReconciliationPostCommitVerificationFailed} {
		f := &reconcileFixture{plan: conflictSyncPlan(t)}
		f.execute = func(ctx context.Context, req workflow.SyncExecutionRequest) (workflow.SyncExecutionResult, error) {
			if req.ApproveCapture == nil || req.ApproveRestore == nil || !req.Terminal || req.ReviewedPlan.Identity.Local != strings.Repeat("b", 40) {
				t.Fatal("execution bypassed fresh review or lost pinned plan")
			}
			if err := req.ApproveCapture(ctx, workflow.CaptureInspection{}); err != nil {
				return workflow.SyncExecutionResult{}, err
			}
			if err := req.ApproveRestore(ctx, model.RestorePlan{}); err != nil {
				return workflow.SyncExecutionResult{}, err
			}
			return workflow.SyncExecutionResult{Published: true}, cause
		}
		s := NewReconcileContext(context.Background(), f)
		s.Update(s.Init()())
		s.Update(tea.KeyPressMsg{Code: 'L'})
		if cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || s.confirming {
			t.Fatal("Enter in the review started the Sync run flow")
		}
		if cmd := s.Update(tea.KeyPressMsg{Code: 's'}); cmd != nil || !strings.Contains(s.View(), "Send approved profile changes to the shared repository") {
			t.Fatal("missing stage confirmation before execution")
		}
		cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("missing execution handoff")
		}
		req, ok := cmd().(TerminalRequest)
		if !ok {
			t.Fatal("execution did not release real terminal")
		}
		var out bytes.Buffer
		req.Command.SetStdin(strings.NewReader("yes\nyes\n"))
		req.Command.SetStdout(&out)
		req.Command.SetStderr(&out)
		if err := req.Command.Run(); err != nil {
			t.Fatal(err)
		}
		refresh := s.Update(req.Done(nil))
		if refresh == nil || s.CanExecute() || strings.Contains(s.View(), "settled through") {
			t.Fatal("failure reused old plan or claimed settlement")
		}
		s.Update(refresh())
		if f.calls != 2 || f.executions != 1 || !strings.Contains(out.String(), "approve the actual Capture and Restore") || !strings.Contains(out.String(), "Save these settings? [y/N]") || strings.Contains(out.String(), "/private/path") {
			t.Fatal("missing refresh/reviews or leaked private failure")
		}
		if cause == profilegit.ErrReconciliationPostCommitVerificationFailed && !strings.Contains(s.View(), "Do not run Sync again") {
			t.Fatal("repair guidance lost")
		}
	}
}
func conflictSyncPlan(t *testing.T) syncengine.Plan {
	t.Helper()
	p, err := syncengine.Reconcile(syncengine.PlanRequest{Identity: syncengine.Identity{Base: strings.Repeat("a", 40), Local: strings.Repeat("b", 40), Remote: strings.Repeat("c", 40), Machine: "desktop", Branch: "main", Upstream: "origin/main", RemoteFingerprint: strings.Repeat("d", 64), ControlFingerprint: strings.Repeat("e", 64)}, Git: syncengine.GitState{Clean: true}, Targets: []syncengine.TargetInput{{Key: syncengine.TargetKey{Category: "defaults", Target: "browser"}, Base: syncengine.State{Kind: syncengine.StatePresent, Fingerprint: "base"}, Local: syncengine.State{Kind: syncengine.StatePresent, Fingerprint: "local"}, Remote: syncengine.State{Kind: syncengine.StatePresent, Fingerprint: "remote"}, Machine: syncengine.State{Kind: syncengine.StatePresent, Fingerprint: "local"}, Policy: syncengine.EffectivePolicy{Capture: true, Restore: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestReconcileInspectionIsLazyAndConflictChoicesAreReadOnly(t *testing.T) {
	f := &reconcileFixture{plan: conflictSyncPlan(t)}
	s := NewReconcileContext(context.Background(), f)
	s.SetSize(80, 18)
	if f.calls != 0 {
		t.Fatal("construction observed or fetched")
	}
	cmd := s.Init()
	if f.calls != 0 || cmd == nil || !strings.Contains(s.View(), "Checking for updates") {
		t.Fatal("initial frame blocked on observation")
	}
	s.Update(cmd())
	if f.calls != 1 || !f.fetch || !strings.Contains(s.View(), "Needs your decision") || !strings.Contains(s.DetailView(), "Defaults · browser") {
		t.Fatal("missing single inspection/conflict details")
	}
	s.Update(tea.KeyPressMsg{Code: 'd'})
	if !strings.Contains(s.View(), "Left for later") || s.CanExecute() {
		t.Fatal("deferred intent shown as executable")
	}
	s.Update(tea.KeyPressMsg{Code: 'L'})
	if !s.CanExecute() || f.calls != 1 || f.executions != 0 {
		t.Fatal("intent resolution reobserved or mutated")
	}
	if !strings.Contains(s.DetailView(), "Your decision: Keep the version saved on this computer") {
		t.Fatal("missing in-session choice")
	}
	fresh := NewReconcileContext(context.Background(), f)
	fresh.Update(fresh.Init()())
	if fresh.CanExecute() {
		t.Fatal("choice persisted across screen recreation")
	}
}
func TestReconcileCancelledOrFailedRefreshCannotExecuteOldPlan(t *testing.T) {
	for _, cause := range []error{context.Canceled, errors.New("private-network-output")} {
		f := &reconcileFixture{plan: conflictSyncPlan(t)}
		s := NewReconcileContext(context.Background(), f)
		s.Update(s.Init()())
		s.Update(tea.KeyPressMsg{Code: 'L'})
		f.err = cause
		s.Update(s.Init()())
		if s.CanExecute() || strings.Contains(s.View(), "private-network-output") || !strings.Contains(s.View(), "couldn't finish checking") {
			t.Fatal("failed inspection retained execution authority or exposed cause")
		}
	}
}

func TestReconcileKeepLocalDoesNotBorrowPaneNavigationKey(t *testing.T) {
	f := &reconcileFixture{plan: conflictSyncPlan(t)}
	s := NewReconcileContext(context.Background(), f)
	s.Update(s.Init()())
	s.Update(tea.KeyPressMsg{Code: 'L'})
	if !strings.Contains(s.DetailView(), "Your decision: Keep the version saved on this computer") {
		t.Fatal("uppercase L action did not resolve; lowercase l belongs to pane navigation")
	}
}

// Details and the footer may only offer a choice that choose then accepts, and
// a decided target keeps describing what changed alongside the decision.
func TestReconcileOfferedChoicesAreExactlyAcceptedChoices(t *testing.T) {
	keepLocal, acceptRemote := "Keep the version saved on this computer", "Use the shared saved version"
	keys := map[string]rune{keepLocal: 'L', acceptRemote: 'a', "Save this computer's current setting": 'm', "Apply the saved setting to this computer": 'p', "Decide later": 'd'}
	f := &reconcileFixture{plan: conflictSyncPlan(t)}
	s := NewReconcileContext(context.Background(), f)
	s.Update(s.Init()())
	offered := map[string]bool{}
	for _, action := range s.Actions() {
		if _, choice := keys[action.Label]; choice && action.Enabled {
			offered[action.Label] = true
		}
	}
	if len(offered) != 3 || !offered[keepLocal] || !offered[acceptRemote] || !offered["Decide later"] {
		t.Fatalf("history conflict offered %v", offered)
	}
	for label, key := range keys {
		trial := NewReconcileContext(context.Background(), f)
		trial.Update(trial.Init()())
		before := trial.current.Items[0]
		trial.Update(tea.KeyPressMsg{Code: key})
		after := trial.current.Items[0]
		changed := after.Resolution != before.Resolution || after.MachineResolution != before.MachineResolution
		if changed != offered[label] || trial.message != "" && offered[label] {
			t.Fatalf("%s offered=%t but accepted=%t", label, offered[label], changed)
		}
	}
	s.Update(tea.KeyPressMsg{Code: 'L'})
	view, detail := s.View(), s.DetailView()
	if !strings.Contains(view, "Decided") || !strings.Contains(detail, "changed differently in two copies of your saved profile") || !strings.Contains(detail, "Your decision: Keep the version saved on this computer") {
		t.Fatalf("decided target lost its group or situation:\n%s\n%s", view, detail)
	}
}

func TestReconcileHistoricalProfileDiagnosticIsReadableAndBlocks(t *testing.T) {
	message := "Pinned local (L) profile records themes theme:aether but its committed artifact is missing (Git cannot store an empty directory). Stop managing theme:aether, set its themes Capture policy to preserve so Capture does not re-add it, then commit and publish"
	f := &reconcileFixture{plan: syncengine.Plan{BootstrapRequired: true}, bootstrap: workflow.SyncBootstrapPlan{Identity: syncengine.Identity{Local: strings.Repeat("b", 40), Remote: strings.Repeat("b", 40), Machine: "laptop", Branch: "main", Upstream: "origin/main"}, Git: syncengine.GitState{Clean: true}, Blockers: []syncengine.Blocker{{Code: "historical-profile", Message: message}}}}
	s := NewReconcileContext(context.Background(), f)
	s.SetSize(64, 20)
	s.Update(s.Init()())
	s.Update(tea.KeyPressMsg{Code: 'u'})
	view := s.View()
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 64 {
			t.Fatalf("diagnostic overflowed: %q", line)
		}
	}
	flat := strings.Join(strings.Fields(view), " ")
	detail := strings.Join(strings.Fields(s.DetailView()), " ")
	if !strings.Contains(flat, "Needs attention before Sync can run (1)") || !strings.Contains(flat, "Can't read a saved profile") || !strings.Contains(detail, "Blueprint could not safely read one of the saved versions") || !strings.Contains(detail, "theme:aether") || !strings.Contains(detail, "Capture policy to preserve") || !strings.Contains(detail, "historical-profile") {
		t.Fatalf("diagnostic hidden:\n%s\n%s", view, s.DetailView())
	}
	if s.CanExecute() || s.bootstrapChoice != nil {
		t.Fatal("blocked historical profile became executable")
	}
}
