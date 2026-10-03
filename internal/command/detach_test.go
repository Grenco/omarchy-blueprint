package command

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunDetachesFromTheTerminalSoNothingCanPrompt(t *testing.T) {
	// A session leader has session id == its pid; a detached command has
	// no controlling terminal for sudo, ssh or git to prompt on.
	out, err := SystemRunner{}.Run(context.Background(), "sh", "-c", `echo "prompt=$GIT_TERMINAL_PROMPT"; [ "$(awk '{print $6}' /proc/$$/stat)" = "$$" ] && echo leader`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "prompt=0") || !strings.Contains(out, "leader") {
		t.Fatalf("command was not detached: %q", out)
	}
}

func TestRunAsksACancelledCommandToStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	out, _ := SystemRunner{}.Run(ctx, "sh", "-c", `trap 'echo stopped; exit 0' TERM; sleep 5 & wait`)
	if !strings.Contains(out, "stopped") {
		t.Fatalf("cancelled command was not sent SIGTERM: %q", out)
	}
}

func TestInterruptDuringInteractiveCommandMeansSkipped(t *testing.T) {
	done := make(chan error, 1)
	go func() { done <- SystemRunner{}.RunInteractive(context.Background(), "sh", "-c", "sleep 0.3; exit 130") }()
	deadline := time.Now().Add(2 * time.Second)
	for !InteractiveActive() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !InteractiveActive() {
		t.Fatal("interactive command was never marked as owning the terminal")
	}
	NoteInterrupt()
	if err := <-done; !errors.Is(err, ErrSkippedByUser) {
		t.Fatalf("interrupted interactive command: err = %v, want ErrSkippedByUser", err)
	}
	if InteractiveActive() {
		t.Fatal("interactive tracking leaked after the command finished")
	}
	if err := (SystemRunner{}).RunInteractive(context.Background(), "sh", "-c", "exit 130"); err == nil || errors.Is(err, ErrSkippedByUser) {
		t.Fatalf("a failure without an interrupt was reported as skipped: %v", err)
	}
}
