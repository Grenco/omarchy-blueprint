package screens

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// SyncExecutionComplete asks the root to refresh other initialized workspaces;
// the originating screen owns its safe receipt and fresh cached inspection.
type SyncExecutionComplete struct {
	Result workflow.SyncExecutionResult
	Err    error
}

// stageSummary is printed to the lent terminal before the separate approvals.
func (s *Reconcile) stageSummary() string {
	return "Blueprint Sync\nBlueprint will check that nothing has changed since your review, then ask you to approve the actual Capture and Restore steps separately. If anything important has changed, it will stop.\n"
}

// Sync borrows the terminal for the explicit transaction, not for observation.
// This preserves ordinary Git authentication and the existing workflow's fresh
// Capture/Restore approval callbacks without teaching Sync about Restore's
// progress/terminal-leasing internals.
func (s *Reconcile) execute() tea.Cmd {
	if !s.CanExecute() {
		return nil
	}
	s.busy, s.ready = true, false
	command := &reconcileExec{ctx: s.ctx, session: s.session, request: workflow.SyncExecutionRequest{ReviewedPlan: s.base, Resolutions: reconcileChoices(s.choices), Terminal: true}, summary: s.stageSummary()}
	if s.bootstrapChoice != nil {
		choice := *s.bootstrapChoice
		reviewed := *s.bootstrap
		command.request.Bootstrap = &choice
		command.request.ReviewedBootstrap = &reviewed
		command.request.Resolutions = nil
	}
	return requestTerminal(command, func(err error) tea.Msg {
		if err != nil && command.err == nil {
			command.err = err
		}
		return SyncExecutionComplete{command.result, command.err}
	})
}

// A failed terminal write invalidates approval: the user may not have seen the
// fresh review. Keep the first error, and never treat subsequent input as assent.
type syncReviewWriter struct {
	io.Writer
	err error
}

func (w *syncReviewWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.Writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

type reconcileExec struct {
	ctx     context.Context
	session reconcileWorkflow
	request workflow.SyncExecutionRequest
	summary string
	in      io.Reader
	out     io.Writer
	result  workflow.SyncExecutionResult
	err     error
}

func (e *reconcileExec) SetStdin(r io.Reader)  { e.in = r }
func (e *reconcileExec) SetStdout(w io.Writer) { e.out = w }
func (e *reconcileExec) SetStderr(io.Writer)   {}
func (e *reconcileExec) Run() error {
	if e.out == nil {
		e.out = io.Discard
	}
	output := &syncReviewWriter{Writer: e.out}
	e.out = output
	if e.in == nil {
		e.err = errors.New("interactive input unavailable")
		return nil
	}
	input := bufio.NewReader(e.in)
	confirm := func(ctx context.Context, label string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "%s? [y/N] ", label)
		if output.err != nil {
			return errors.New("Sync review output unavailable")
		}
		line, err := input.ReadString('\n')
		if err != nil {
			return errors.New("Sync approval cancelled")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if value := strings.ToLower(strings.TrimSpace(line)); value != "y" && value != "yes" {
			return errors.New("Sync approval declined")
		}
		return nil
	}
	fmt.Fprintln(e.out, e.summary)
	if output.err != nil {
		e.err = errors.New("Sync review output unavailable")
		return nil
	}
	e.request.ApproveCapture = func(ctx context.Context, review workflow.CaptureInspection) error {
		fmt.Fprintln(e.out, "Blueprint wants to save these settings from this computer into your profile:")
		for _, section := range review.Review() {
			for _, target := range section.Targets {
				fmt.Fprintf(e.out, "%s:%s — %s\n", components.DisplayText(target.Category), components.DisplayText(target.Inspection.Key), target.Outcome.Label())
			}
		}
		return confirm(ctx, "Save these settings")
	}
	e.request.ApproveRestore = func(ctx context.Context, plan model.RestorePlan) error {
		review := e.restoreReview(ctx, plan)
		review.compatibilityExpanded = true
		fmt.Fprintln(e.out, "Blueprint wants to apply these saved settings to this computer. This may replace or remove settings on this computer.", review.View(), review.DetailView())
		for i := range plan.Operations {
			review.selected = i
			review.compatibilityExpanded = false
			fmt.Fprintln(e.out, review.DetailView())
		}
		if err := workflow.CheckRestoreApplicable(plan, true); err != nil {
			return err
		}
		return confirm(ctx, review.confirmation()+" Apply these changes to this computer")
	}
	e.request.ReviewActivation = func(ctx context.Context, candidates []model.ActivationCandidate) (*policy.ActivationReviewSelection, error) {
		selection := &policy.ActivationReviewSelection{}
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			fmt.Fprintf(e.out, "Activate the service %s now? %s [y/N] ", components.DisplayText(candidate.Unit), components.DisplayText(candidate.Reason))
			if output.err != nil {
				return nil, errors.New("Services activation review output unavailable")
			}
			line, err := input.ReadString('\n')
			if err != nil {
				return nil, errors.New("Services activation review cancelled")
			}
			if answer := strings.ToLower(strings.TrimSpace(line)); answer == "y" || answer == "yes" {
				selection.Units = append(selection.Units, candidate.Unit)
			}
		}
		return selection, nil
	}
	e.result, e.err = e.session.ExecuteSync(e.ctx, e.request)
	if e.err != nil {
		fmt.Fprintln(e.out, reconcileFailure(e.err))
	} else if !e.result.CursorAdvanced {
		fmt.Fprintln(e.out, reconcileIncomplete)
	} else {
		fmt.Fprintln(e.out, reconcileCompleted)
	}
	// Only the safe receipt crosses the presentation boundary; tea.Exec must
	// not render a raw wrapped transport/provider error itself.
	return nil
}

func (e *reconcileExec) restoreReview(ctx context.Context, plan model.RestorePlan) *Restore {
	review := NewRestoreContext(ctx, nil)
	// Freeze descriptive context for this approval, not another observation or
	// mutation authority. ExecuteSync revalidated the selected identity and
	// owns the fresh authoritative plan. Do not replan just to count overrides.
	snapshot := workflow.ReadSnapshot{Machine: machine.Selection{Name: e.request.ReviewedPlan.Identity.Machine}}
	review.current, review.width = plan, 100
	if session, ok := e.session.(*workflow.Session); ok {
		snapshot.Profile, snapshot.Machine = session.Profile(), session.Machine()
		if selected := snapshot.Machine.Machine; selected != nil {
			review.options = selected.EffectiveRestoreDefaults()
		}
	}
	if e.request.RestoreOptions != nil {
		review.options, review.override = *e.request.RestoreOptions, true
	}
	review.acceptSnapshot(snapshot)
	review.forcedOverridesUnavailable = review.options.Conflicts == policy.ConflictForce
	return review
}

// reconcileFailure keeps each failure's safety distinction: repository or
// profile repair stops further Sync runs; anything else may have left
// published or partial work and needs a fresh check before retrying.
func reconcileFailure(err error) string {
	if errors.Is(err, profilegit.ErrReconciliationPostCommitVerificationFailed) || errors.Is(err, profilegit.ErrReconciliationRollbackFailed) || errors.Is(err, profilegit.ErrReconciliationOutcomeUncertain) {
		return "Sync stopped because Blueprint could not confirm the state of your profile repository. Do not run Sync again until the repository has been checked and repaired: check the profile's Git HEAD, index and worktree first. Blueprint has not marked this computer as up to date."
	}
	if errors.Is(err, syncengine.ErrProfileBusy) {
		return "Sync did not start: another Sync or an Auto Sync check is using this profile right now. Nothing was changed. Wait a moment, then try again."
	}
	if errors.Is(err, workflow.ErrScopedCaptureRepairRequired) {
		return "Sync stopped because Blueprint could not safely finish updating your saved profile. Do not run Sync again until the profile has been checked and repaired. Blueprint has not marked this computer as up to date."
	}
	return reconcileIncomplete + " Changes may already have been sent to the shared profile, or some settings on this computer may have been partly applied. Press r to check for updates before trying again."
}
