package main

import (
	"context"
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
	go handleInterrupts(signals, command.InteractiveActive, cancel, os.Exit)
	os.Exit(app.Execute(ctx, os.Args[1:], app.Dependencies{}))
}

// handleInterrupts decides what Ctrl+C means (ADR 0028). While a step that
// owns the terminal runs (a sudo prompt, a sign-in), the terminal has
// already delivered the interrupt to it; Blueprint leaves it there, so only
// that step stops and Restore continues. Otherwise an interrupt stops
// Blueprint: it cancels the context, which stops the running command
// gracefully, and a second interrupt exits at once.
func handleInterrupts(signals <-chan os.Signal, interactive func() bool, cancel context.CancelFunc, exit func(int)) {
	stopping := false
	for sig := range signals {
		if sig == os.Interrupt && interactive() {
			command.NoteInterrupt()
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
