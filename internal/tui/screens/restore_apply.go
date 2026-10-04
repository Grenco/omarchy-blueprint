package screens

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// An approved plan always applies from the interface (ADR 0028). Steps
// that need the person (a sudo password, a sign-in) borrow the real
// terminal one at a time through a terminalLease, and the interface comes
// back as soon as the next ordinary step starts, so Restore never
// disappears for the whole run.

// restoreRunState is what an in-interface apply shares with the screen. The
// apply goroutine updates it and only nudges the screen to redraw, so a
// lent terminal (which pauses the interface) can never stall the restore.
type restoreRunState struct {
	mu                           sync.Mutex
	total, done, failed, skipped int
	current                      string
	elapsed                      time.Duration
	inTerminal                   bool
}

type restoreRunSnapshot struct {
	total, done, failed, skipped int
	current                      string
	elapsed                      time.Duration
	inTerminal                   bool
}

func (r *restoreRunState) snapshot() restoreRunSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return restoreRunSnapshot{r.total, r.done, r.failed, r.skipped, r.current, r.elapsed, r.inTerminal}
}

func (r *restoreRunState) record(event restore.Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch event.Type {
	case restore.ProgressStarted:
		r.current, r.elapsed = restoreStepLabel(event.Operation), 0
	case restore.ProgressHeartbeat:
		r.elapsed = event.Elapsed
	case restore.ProgressCompleted:
		r.done++
	case restore.ProgressFailed:
		r.done++
		r.failed++
	case restore.ProgressSkipped:
		r.done++
		r.skipped++
	}
}

func (r *restoreRunState) setInTerminal(in bool) {
	r.mu.Lock()
	r.inTerminal = in
	r.mu.Unlock()
}

// restoreEventMsg wakes the screen: the shared state changed, or a step
// asks for the terminal (lease). Either way the screen keeps listening.
type restoreEventMsg struct {
	lease  *terminalHold
	events <-chan restoreEventMsg
}

func waitRestoreEvents(events <-chan restoreEventMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return nil
		}
		msg.events = events
		return msg
	}
}

// terminalHold is one loan of the terminal: an exec command that holds the
// terminal until released.
type terminalHold struct {
	out      io.Writer
	intro    string
	granted  chan struct{}
	released chan struct{}
}

func (h *terminalHold) Run() error {
	if h.out != nil && h.intro != "" {
		fmt.Fprintln(h.out, h.intro)
	}
	close(h.granted)
	<-h.released
	return nil
}
func (*terminalHold) SetStdin(io.Reader)      {}
func (h *terminalHold) SetStdout(w io.Writer) { h.out = w }
func (*terminalHold) SetStderr(io.Writer)     {}

// terminalLease lends the terminal to interactive steps, keeping it across
// consecutive ones so the screen doesn't flicker between them.
type terminalLease struct {
	events  chan<- restoreEventMsg
	mu      sync.Mutex
	hold    *terminalHold
	pending string // the start line of the step about to borrow the terminal
}

const terminalLeaseIntro = "Blueprint lent the terminal to steps that need you; it comes back when they finish.\nCtrl+C while a step is asking you something skips just that step; the rest of the restore continues."

func (l *terminalLease) acquire(ctx context.Context) error {
	l.mu.Lock()
	if l.hold != nil {
		l.mu.Unlock()
		return nil
	}
	hold := &terminalHold{intro: terminalLeaseIntro, granted: make(chan struct{}), released: make(chan struct{})}
	if l.pending != "" {
		hold.intro += "\n" + l.pending
	}
	l.hold = hold
	l.mu.Unlock()
	select {
	case l.events <- restoreEventMsg{lease: hold}:
	case <-ctx.Done():
		l.release()
		return ctx.Err()
	}
	select {
	case <-hold.granted:
		return nil
	case <-ctx.Done():
		l.release()
		return ctx.Err()
	}
}

func (l *terminalLease) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hold != nil {
		close(l.hold.released)
		l.hold = nil
	}
}

// print writes a progress line to the terminal while it is lent, and
// remembers an interactive step's start line for the next loan.
func (l *terminalLease) print(event restore.Progress, line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hold == nil {
		if event.Type == restore.ProgressStarted && event.Operation.Interactive {
			l.pending = line
		}
		return
	}
	if line != "" && l.hold.out != nil {
		fmt.Fprintln(l.hold.out, line)
	}
}

// apply runs the approved plan from the interface. The workflow refuses a
// plan that changed since approval.
func (s *Restore) apply() tea.Cmd {
	var options *policy.RestoreOptions
	if s.override {
		copied := s.options
		options = &copied
	}
	approved := s.plan()
	state := &restoreRunState{total: len(approved.Operations)}
	s.run, s.stopRequested = state, false
	events := make(chan restoreEventMsg, 16)
	lease := &terminalLease{events: events}
	ctx, cancel := context.WithCancel(s.ctx)
	s.stopApply = cancel
	session, scope := s.session, s.scope()
	nudge := func() {
		select {
		case events <- restoreEventMsg{}:
		default: // a redraw is already pending
		}
	}
	run := func() tea.Msg {
		defer close(events)
		defer cancel()
		if session == nil {
			return restoreAppliedMsg{err: fmt.Errorf("restore session is unavailable")}
		}
		result, err := session.ApplyApprovedRestoreWithHooks(ctx, scope, options, approved, true, workflow.RestoreApplyHooks{
			Progress: func(event restore.Progress) {
				if event.Type == restore.ProgressStarted && !event.Operation.Interactive {
					lease.release()
				}
				state.record(event)
				lease.print(event, restoreProgressLine(event))
				nudge()
			},
			Interactive: func(ctx context.Context, run func() error) error {
				if err := lease.acquire(ctx); err != nil {
					return err
				}
				state.setInTerminal(true)
				defer state.setInTerminal(false)
				return run()
			},
		})
		lease.release()
		return restoreAppliedMsg{result, err}
	}
	return tea.Batch(run, waitRestoreEvents(events))
}

// handleRestoreEvent redraws for progress, or lends the terminal when a
// step asks for it, and keeps listening either way.
func (s *Restore) handleRestoreEvent(msg restoreEventMsg) tea.Cmd {
	listen := waitRestoreEvents(msg.events)
	if msg.lease == nil {
		return listen
	}
	return tea.Batch(s.exec(msg.lease, func(error) tea.Msg { return nil }), listen)
}

// Applying reports whether an approved plan is being applied.
func (s *Restore) Applying() bool { return s.busy }

// InterruptApply is Ctrl+C while a restore applies in the interface: the
// first press explains, the second stops the restore. The running command
// is asked to stop (a local file operation finishes), Blueprint waits for
// it to settle, and no further step starts. Quitting outright would abandon
// a running step.
func (s *Restore) InterruptApply() string {
	if !s.busy {
		return ""
	}
	if !s.stopRequested {
		s.stopRequested = true
		return "A restore is running. Press Ctrl+C again to stop it: the running step is asked to stop, and no further step starts."
	}
	if s.stopApply != nil {
		s.stopApply()
	}
	return "Stopping the restore: waiting for the running step to stop…"
}
