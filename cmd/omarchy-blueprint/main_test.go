package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

func TestInterruptSkipsAnInteractiveStepOtherwiseStopsThenExits(t *testing.T) {
	signals := make(chan os.Signal, 4)
	// A step owns the terminal for the first interrupt only.
	checks, cancelled, exited := 0, 0, -1
	interactive := func() bool { checks++; return checks == 1 }
	done := make(chan struct{})
	go func() {
		handleInterrupts(signals, interactive, func() { cancelled++ }, func(code int) { exited = code })
		close(done)
	}()
	signals <- os.Interrupt // a step owns the terminal: leave it to the step
	signals <- syscall.SIGTERM
	signals <- os.Interrupt
	close(signals)
	<-done
	if cancelled != 1 || exited != 130 {
		t.Fatalf("cancelled=%d exited=%d; want the step's interrupt ignored, SIGTERM to cancel and the next interrupt to exit", cancelled, exited)
	}
}

// A step that owns the terminal gets the first interrupts; one that ignores
// them (or hangs) cannot hold Blueprint hostage: the next interrupt stops
// the restore and a further one exits.
func TestInterruptsEscalateOverAStepThatIgnoresThem(t *testing.T) {
	signals := make(chan os.Signal, 8)
	presses, cancelled, exited := 0, 0, -1
	leftToStep := func() bool { presses++; return presses <= interruptsLeftToAStep }
	done := make(chan struct{})
	go func() {
		handleInterrupts(signals, leftToStep, func() { cancelled++ }, func(code int) { exited = code })
		close(done)
	}()
	for range interruptsLeftToAStep {
		signals <- os.Interrupt
	}
	signals <- os.Interrupt // the step ignored them: stop the restore
	signals <- os.Interrupt // and exit if that is not enough
	close(signals)
	<-done
	if cancelled != 1 || exited != 130 {
		t.Fatalf("cancelled=%d exited=%d; want the step's interrupts left to it, then a stop, then exit", cancelled, exited)
	}
}

// End to end with a real interactive command that never sees the terminal's
// interrupts (here, none are delivered to it): the first interrupts are
// left to it, the next cancels the context and RunInteractive stops it.
func TestRealInteractiveStepIsStoppedAfterIgnoredInterrupts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready")
	finished := make(chan error, 1)
	go func() {
		finished <- command.SystemRunner{}.RunInteractive(ctx, "sh", "-c", `echo ok > "$1"; exec sleep 30`, "sh", ready)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for _, err := os.Stat(ready); err != nil; _, err = os.Stat(ready) {
		if time.Now().After(deadline) {
			t.Fatal("interactive command never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	signals := make(chan os.Signal, 8)
	exited := -1
	done := make(chan struct{})
	go func() {
		handleInterrupts(signals, interactiveInterrupt, cancel, func(code int) { exited = code })
		close(done)
	}()
	for range interruptsLeftToAStep {
		signals <- os.Interrupt
	}
	time.Sleep(100 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("an interrupt left to the step stopped Blueprint")
	}
	signals <- os.Interrupt
	select {
	case err := <-finished:
		if err == nil || errors.Is(err, command.ErrSkippedByUser) {
			t.Fatalf("stopped step reported %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Blueprint stayed stuck on a step that ignored interrupts")
	}
	close(signals)
	<-done
	if exited != -1 {
		t.Fatalf("exited=%d; stopping the restore is not exiting", exited)
	}
}
