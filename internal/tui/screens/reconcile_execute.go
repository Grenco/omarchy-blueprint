package screens

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// SyncExecutionComplete asks the root to refresh other initialized workspaces;
// the originating screen owns its safe receipt and fresh cached inspection.
type SyncExecutionComplete struct {
	Result workflow.SyncExecutionResult
	Err    error
}

func (s *Reconcile) stageSummary() string {
	return fmt.Sprintf("Pinned B %s · L %s · R %s\nRevalidate → reconcile pinned history as needed → selected Capture (fresh approval) → Publish if needed → selected Restore (fresh approval) → Verify → record cursor", shortRevision(s.current.Identity.Base), shortRevision(s.current.Identity.Local), shortRevision(s.current.Identity.Remote))
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
		fmt.Fprintln(e.out, "Review fresh selected Capture")
		for _, section := range review.Review() {
			for _, target := range section.Targets {
				fmt.Fprintf(e.out, "%s:%s — %s\n", components.DisplayText(target.Category), components.DisplayText(target.Inspection.Key), target.Outcome.Label())
			}
		}
		return confirm(ctx, "Approve this fresh Capture")
	}
	e.request.ApproveRestore = func(ctx context.Context, plan model.RestorePlan) error {
		review := NewRestoreContext(ctx, nil)
		review.current = plan
		review.width = 100
		review.options = policy.DefaultRestoreOptions()
		if session, ok := e.session.(*workflow.Session); ok {
			if machine := session.Machine().Machine; machine != nil {
				review.options = machine.EffectiveRestoreDefaults()
			}
		}
		review.compatibilityExpanded = true
		fmt.Fprintln(e.out, "Review fresh Restore", review.View(), review.DetailView())
		for i := range plan.Operations {
			review.selected = i
			review.compatibilityExpanded = false
			fmt.Fprintln(e.out, review.DetailView())
		}
		if err := workflow.CheckRestoreApplicable(plan, true); err != nil {
			return err
		}
		return confirm(ctx, review.confirmation()+" Approve this fresh Restore")
	}
	e.request.ReviewActivation = func(ctx context.Context, candidates []model.ActivationCandidate) (*policy.ActivationReviewSelection, error) {
		selection := &policy.ActivationReviewSelection{}
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			fmt.Fprintf(e.out, "Activate %s? %s [y/N] ", components.DisplayText(candidate.Unit), components.DisplayText(candidate.Reason))
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
		fmt.Fprintln(e.out, "Settlement was not recorded; inspect before retrying.")
	} else {
		fmt.Fprintln(e.out, "Settlement recorded. Returning to a fresh inspection.")
	}
	// Only the safe receipt crosses the presentation boundary; tea.Exec must
	// not render a raw wrapped transport/provider error itself.
	return nil
}

func reconcileFailure(err error) string {
	if errors.Is(err, profilegit.ErrReconciliationPostCommitVerificationFailed) || errors.Is(err, profilegit.ErrReconciliationRollbackFailed) || errors.Is(err, profilegit.ErrReconciliationOutcomeUncertain) {
		return "Do not retry Sync. HIGH SEVERITY: repository requires manual validation/repair; verify HEAD, index and worktree first. Cursor was not advanced."
	}
	if errors.Is(err, workflow.ErrScopedCaptureRepairRequired) {
		return "Do not retry Sync. HIGH SEVERITY: profile recovery failed; manually repair and validate the profile first. Cursor was not advanced."
	}
	return "Sync execution stopped; cursor was not advanced. Inspect again before retrying; published history or partial machine changes may remain."
}
