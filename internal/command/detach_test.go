package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sessionProbe prints GIT_TERMINAL_PROMPT and whether the shell leads its
// own session (session id == pid), i.e. was detached from the terminal.
const sessionProbe = `echo "prompt=$GIT_TERMINAL_PROMPT"; [ "$(awk '{print $6}' /proc/$$/stat)" = "$$" ] && echo leader || echo member`

func TestRunDetachedCannotPrompt(t *testing.T) {
	out, err := SystemRunner{}.RunDetached(context.Background(), "sh", "-c", sessionProbe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "prompt=0") || !strings.Contains(out, "leader") {
		t.Fatalf("Restore command was not detached: %q", out)
	}
}

// Ordinary Run is used well beyond Restore (profile Git sync among them),
// where SSH or Git may legitimately prompt on the terminal. It must not be
// detached.
func TestRunKeepsTheTerminalForEveryOtherCaller(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "")
	for name, run := range map[string]func() (string, error){
		"Run": func() (string, error) { return SystemRunner{}.Run(context.Background(), "sh", "-c", sessionProbe) },
		"RunOutput": func() (string, error) {
			out, err := SystemRunner{}.RunOutput(context.Background(), 1024, "sh", "-c", sessionProbe)
			return string(out), err
		},
	} {
		out, err := run()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "prompt=0") || !strings.Contains(out, "member") {
			t.Fatalf("%s detached its command: %q", name, out)
		}
	}
}

func TestRunNonInteractivePrefersDetachedExecution(t *testing.T) {
	out, err := RunNonInteractive(context.Background(), SystemRunner{}, "sh", "-c", sessionProbe)
	if err != nil || !strings.Contains(out, "leader") {
		t.Fatalf("RunNonInteractive did not detach: %q %v", out, err)
	}
}

func TestCancelledDetachedCommandsWholeGroupIsGone(t *testing.T) {
	previous := detachedGrace
	detachedGrace = 300 * time.Millisecond
	t.Cleanup(func() { detachedGrace = previous })
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	// The command and its helper both ignore SIGTERM, as a stubborn helper
	// (an ssh, say) might; only killing the whole group stops the helper.
	script := `trap '' TERM; sleep 30 & echo $! > "$1"; wait`
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			if _, err := os.Stat(pidFile); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	started := time.Now()
	if _, err := (SystemRunner{}).RunDetached(ctx, "sh", "-c", script, "sh", pidFile); err == nil {
		t.Fatal("a cancelled command reported success")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("cancellation took %s; the grace period was not enforced", elapsed)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if stat, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); strings.Contains(string(stat), ") Z ") {
			break // killed; only the zombie entry is left until it's reaped
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper %d that ignored SIGTERM survived the cancelled restore", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCancelledDetachedCommandIsAskedToStopFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	out, _ := SystemRunner{}.RunDetached(ctx, "sh", "-c", `trap 'echo stopped; exit 0' TERM; sleep 5 & wait`)
	if !strings.Contains(out, "stopped") {
		t.Fatalf("cancelled command was not sent SIGTERM first: %q", out)
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
