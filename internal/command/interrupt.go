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
	// interactiveStart is the interrupt count when the current interactive
	// command took the terminal, so NoteInterrupt can count its own.
	interactiveStart atomic.Int64
)

// ErrSkippedByUser marks an interactive command the person stopped with
// Ctrl+C. It is a choice, not a failure.
var ErrSkippedByUser = errors.New("skipped: you pressed Ctrl+C")

// InteractiveActive reports whether an interactive command owns the
// terminal now.
func InteractiveActive() bool { return interactiveDepth.Load() > 0 }

// NoteInterrupt records an interrupt that arrived while an interactive
// command owned the terminal, and returns how many have arrived during that
// command, so the caller can tell a command that ignores them.
func NoteInterrupt() int64 { return interrupts.Add(1) - interactiveStart.Load() }

// runInteractive tracks fn as owning the terminal and reports whether an
// interrupt arrived while it did.
func runInteractive(fn func() error) (err error, interrupted bool) {
	before := interrupts.Load()
	interactiveStart.Store(before)
	interactiveDepth.Add(1)
	defer interactiveDepth.Add(-1)
	err = fn()
	return err, interrupts.Load() != before
}
