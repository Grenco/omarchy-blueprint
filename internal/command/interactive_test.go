package command

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func lowerInteractiveGrace(t *testing.T) {
	t.Helper()
	previous := interactiveGrace
	interactiveGrace = 200 * time.Millisecond
	t.Cleanup(func() { interactiveGrace = previous })
}

func waitForFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return strings.TrimSpace(string(b))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
	return ""
}

func runInteractiveAsync(ctx context.Context, script string, args ...string) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- SystemRunner{}.RunInteractive(ctx, "sh", append([]string{"-c", script, "sh"}, args...)...)
	}()
	return done
}

// A cancelled restore must not wait forever on an interactive command that
// ignores every stop signal, nor on a descendant that keeps the terminal's
// descriptors open after it.
func TestCancelledInteractiveCommandThatIgnoresSignalsIsStopped(t *testing.T) {
	lowerInteractiveGrace(t)
	dir := t.TempDir()
	ready, descendant := filepath.Join(dir, "ready"), filepath.Join(dir, "descendant")
	script := `trap '' INT TERM; (trap '' INT TERM; exec sleep 30) & echo $! > "$2"; echo ok > "$1"; wait`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runInteractiveAsync(ctx, script, ready, descendant)
	waitForFile(t, ready)
	if pid, err := strconv.Atoi(waitForFile(t, descendant)); err == nil {
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || errors.Is(err, ErrSkippedByUser) {
			t.Fatalf("a stopped command reported %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunInteractive waited on a command that ignores interrupts")
	}
	if InteractiveActive() {
		t.Fatal("interactive tracking leaked after the command was stopped")
	}
}

// Cancellation is graceful first: the interrupt, then SIGTERM, and only
// then a kill, so a package manager is never killed outright.
func TestCancelledInteractiveCommandIsInterruptedThenTerminated(t *testing.T) {
	lowerInteractiveGrace(t)
	dir := t.TempDir()
	ready, log := filepath.Join(dir, "ready"), filepath.Join(dir, "log")
	script := `trap 'echo int >> "$2"' INT; trap 'echo term >> "$2"; exit 0' TERM; echo ok > "$1"; while :; do sleep 0.05; done`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runInteractiveAsync(ctx, script, ready, log)
	waitForFile(t, ready)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunInteractive did not stop")
	}
	if got := waitForFile(t, log); got != "int\nterm" {
		t.Fatalf("signals = %q, want the interrupt then SIGTERM", got)
	}
}

// A cooperative command stops at the first signal and is never escalated.
func TestCancelledCooperativeInteractiveCommandOnlyGetsTheInterrupt(t *testing.T) {
	lowerInteractiveGrace(t)
	dir := t.TempDir()
	ready, log := filepath.Join(dir, "ready"), filepath.Join(dir, "log")
	script := `trap 'echo int >> "$2"; exit 130' INT; trap 'echo term >> "$2"' TERM; echo ok > "$1"; while :; do sleep 0.05; done`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runInteractiveAsync(ctx, script, ready, log)
	waitForFile(t, ready)
	cancel()
	<-done
	time.Sleep(3 * interactiveGrace)
	if got := waitForFile(t, log); got != "int" {
		t.Fatalf("signals = %q, want only the interrupt", got)
	}
}

func TestNoteInterruptCountsPerInteractiveCommand(t *testing.T) {
	for run := range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		ready := filepath.Join(t.TempDir(), "ready")
		done := runInteractiveAsync(ctx, `echo ok > "$1"; sleep 30`, ready)
		waitForFile(t, ready)
		for want := int64(1); want <= 3; want++ {
			if got := NoteInterrupt(); got != want {
				t.Fatalf("run %d: interrupt %d counted as %d", run, want, got)
			}
		}
		cancel()
		<-done
	}
}
