package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Grenco/omarchy-blueprint/internal/app"
	"github.com/Grenco/omarchy-blueprint/internal/command"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go handleInterrupts(signals, interactiveInterrupt, cancel, os.Exit)
	os.Exit(app.Execute(ctx, os.Args[1:], app.Dependencies{}))
}

// interruptsLeftToAStep is how many interrupts a step that owns the
// terminal receives before Blueprint stops anyway: one that ignores them
// (or hangs) must not hold Blueprint hostage.
const interruptsLeftToAStep = 2

// interactiveInterrupt reports whether an interrupt belongs to a step that
// owns the terminal, warning on the last one left to it.
func interactiveInterrupt() bool {
	if !command.InteractiveActive() {
		return false
	}
	n := command.NoteInterrupt()
	if n == interruptsLeftToAStep {
		fmt.Fprintln(os.Stderr, "\nBlueprint: the step has not stopped. Press Ctrl+C again to stop the restore.")
	}
	return n <= interruptsLeftToAStep
}

// handleInterrupts decides what Ctrl+C means (ADR 0028). While a step that
// owns the terminal runs (a sudo prompt, a sign-in), the terminal has
// already delivered the interrupt to it; Blueprint leaves it there, so only
// that step stops and Restore continues. Otherwise, or once a step has
// ignored interruptsLeftToAStep of them, an interrupt stops Blueprint: it
// cancels the context, which stops the running command (escalating if it
// will not stop), and a further interrupt exits at once.
func handleInterrupts(signals <-chan os.Signal, leftToStep func() bool, cancel context.CancelFunc, exit func(int)) {
	stopping := false
	for sig := range signals {
		if sig == os.Interrupt && leftToStep() {
			continue
		}
		if stopping {
			exit(130)
			return
		}
		stopping = true
		cancel()
	}
}
