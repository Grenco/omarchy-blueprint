package command

import (
	"errors"
	"sync/atomic"
)

// While an interactive command owns the terminal, an interrupt (Ctrl+C)
// belongs to it: the terminal delivers it to the command, which stops, and
// the step is skipped while the rest of Restore continues (ADR 0028). The
// process's signal handler asks InteractiveActive whether to leave an
// interrupt to the command, and records it with NoteInterrupt.
var (
	interactiveDepth atomic.Int64
	interrupts       atomic.Int64
)

// ErrSkippedByUser marks an interactive command the person stopped with
// Ctrl+C. It is a choice, not a failure.
var ErrSkippedByUser = errors.New("skipped: you pressed Ctrl+C")

// InteractiveActive reports whether an interactive command owns the
// terminal now.
func InteractiveActive() bool { return interactiveDepth.Load() > 0 }

// NoteInterrupt records an interrupt left to the interactive command.
func NoteInterrupt() { interrupts.Add(1) }

// runInteractive tracks fn as owning the terminal and reports whether an
// interrupt arrived while it did.
func runInteractive(fn func() error) (err error, interrupted bool) {
	before := interrupts.Load()
	interactiveDepth.Add(1)
	defer interactiveDepth.Add(-1)
	err = fn()
	return err, interrupts.Load() != before
}
