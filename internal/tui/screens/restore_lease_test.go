package screens

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

// The lease asks the interface for the terminal, holds it across
// consecutive interactive steps, writes their progress there, and gives it
// back when released.
func TestTerminalLeaseLendsTheTerminalUntilReleased(t *testing.T) {
	events := make(chan restoreEventMsg, 4)
	lease := &terminalLease{events: events}
	tailscale := model.Operation{Label: "Omarchy's Tailscale setup", AwaitsYou: "sign in", Interactive: true}
	lease.print(restore.Progress{Type: restore.ProgressStarted, Operation: tailscale}, restoreProgressLine(restore.Progress{Type: restore.ProgressStarted, Operation: tailscale}))

	acquired := make(chan error, 1)
	go func() { acquired <- lease.acquire(context.Background()) }()
	request := <-events
	if request.lease == nil {
		t.Fatal("acquire did not ask the interface for the terminal")
	}
	var terminal bytes.Buffer
	request.lease.SetStdout(&terminal)
	ran := make(chan error, 1)
	go func() { ran <- request.lease.Run() }() // what tea.Exec does
	if err := <-acquired; err != nil {
		t.Fatal(err)
	}
	if err := lease.acquire(context.Background()); err != nil || len(events) != 0 {
		t.Fatalf("a second interactive step asked for the terminal again (err=%v, requests=%d)", err, len(events))
	}
	lease.print(restore.Progress{Type: restore.ProgressSkipped, Operation: tailscale}, restoreProgressLine(restore.Progress{Type: restore.ProgressSkipped, Operation: tailscale}))
	lease.release()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("releasing did not give the terminal back")
	}
	out := terminal.String()
	for _, want := range []string{"Blueprint lent the terminal", "→ Omarchy's Tailscale setup", "↷ Skipped Omarchy's Tailscale setup"} {
		if !strings.Contains(out, want) {
			t.Fatalf("terminal output lacks %q:\n%s", want, out)
		}
	}
}

func TestTerminalLeaseGivesUpWhenTheRestoreStops(t *testing.T) {
	lease := &terminalLease{events: make(chan restoreEventMsg)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := lease.acquire(ctx); err == nil {
		t.Fatal("acquire waited for a terminal after the restore stopped")
	}
}

func TestLeaseRequestIsLentThroughTheRoot(t *testing.T) {
	screen := NewRestore(nil)
	var lent tea.ExecCommand
	screen.exec = func(command tea.ExecCommand, _ tea.ExecCallback) tea.Cmd {
		lent = command
		return nil
	}
	hold := &terminalHold{granted: make(chan struct{}), released: make(chan struct{})}
	events := make(chan restoreEventMsg)
	close(events)
	screen.Update(restoreEventMsg{lease: hold, events: events})
	if lent != hold {
		t.Fatalf("lease request lent %#v, want the hold", lent)
	}
}

func TestCtrlCStopsARunningRestoreOnlyWhenPressedTwice(t *testing.T) {
	screen := NewRestore(nil)
	screen.busy = true
	stopped := false
	screen.stopApply = func() { stopped = true }
	if notice := screen.InterruptApply(); !strings.Contains(notice, "again") || stopped {
		t.Fatalf("first Ctrl+C: notice=%q stopped=%t", notice, stopped)
	}
	if notice := screen.InterruptApply(); !strings.Contains(notice, "Stopping") || !stopped {
		t.Fatalf("second Ctrl+C: notice=%q stopped=%t", notice, stopped)
	}
	screen.Update(restoreAppliedMsg{err: context.Canceled})
	if got := strings.Join(screen.lastRunLines(120, restoreLastRunLines), "\n"); !strings.Contains(got, "stopped by you") {
		t.Fatalf("stopped run: %q", got)
	}
}
