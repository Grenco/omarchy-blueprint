package main

import (
	"os"
	"syscall"
	"testing"
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
