package command

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/diagnostics"
)

// DetachedRunner runs a command that must never prompt (ADR 0028). Only
// Restore's executor uses it, for its non-interactive operations: behind
// the TUI a prompt would be invisible, so it must fail instead of waiting.
// Every other caller, including profile Git sync, keeps Run's ordinary
// terminal behaviour.
type DetachedRunner interface {
	RunDetached(ctx context.Context, name string, args ...string) (string, error)
}

// RunNonInteractive runs a non-interactive Restore command detached when
// the runner supports it, and through Run otherwise.
func RunNonInteractive(ctx context.Context, runner Runner, name string, args ...string) (string, error) {
	if detached, ok := runner.(DetachedRunner); ok {
		return detached.RunDetached(ctx, name, args...)
	}
	return runner.Run(ctx, name, args...)
}

// detachedGrace is how long a cancelled detached command's process group
// has to stop after SIGTERM before the whole group is killed.
var detachedGrace = 10 * time.Second

// RunDetached runs the command in its own session, with no controlling
// terminal and GIT_TERMINAL_PROMPT=0, so sudo, ssh and git fail at once
// instead of prompting. The command leads its own process group; on
// cancellation the whole group (helpers such as git's ssh included) gets
// SIGTERM, then SIGKILL after detachedGrace, so nothing it started outlives
// the restore that was stopped. A group member left running after a normal
// exit is not killed (it may be a service the command started on purpose).
func (SystemRunner) RunDetached(ctx context.Context, name string, args ...string) (string, error) {
	if finish := diagnostics.StartCommand(ctx, name, args); finish != nil {
		defer finish()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	// Don't wait indefinitely for output pipes held open by a lingering
	// group member once the command itself has exited.
	cmd.WaitDelay = detachedGrace
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return "", &RunError{Name: name, Args: args, ExitCode: -1, Err: err}
	}
	group := -cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(group, syscall.SIGTERM)
		exited := false
		select {
		case err = <-done:
			exited = true
		case <-time.After(detachedGrace):
		}
		// Whatever ignored SIGTERM, including anything still holding the
		// group after the command itself exited, is killed now.
		_ = syscall.Kill(group, syscall.SIGKILL)
		if !exited {
			err = <-done
		}
		if err == nil {
			err = ctx.Err()
		}
	}
	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return out.String(), &RunError{Name: name, Args: args, Output: out.String(), ExitCode: exitCode, Err: err}
	}
	return out.String(), nil
}
