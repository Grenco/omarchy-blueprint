package command

import (
	"context"
	"errors"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/diagnostics"
	"os"
	"os/exec"
	"strings"
)

// Runner implementations used by read observation must support concurrent
// calls from independent providers. Authoritative mutation remains serialized.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

type OutputRunner interface {
	RunOutput(ctx context.Context, limit int64, name string, args ...string) ([]byte, error)
}

// InteractiveRunner executes a command attached to the process terminal so
// prompts, authentication URLs, and user input remain visible and usable.
type InteractiveRunner interface {
	RunInteractive(ctx context.Context, name string, args ...string) error
}

func RunOutput(ctx context.Context, runner Runner, limit int64, name string, args ...string) ([]byte, error) {
	if r, ok := runner.(OutputRunner); ok {
		return r.RunOutput(ctx, limit, name, args...)
	}
	out, err := runner.Run(ctx, name, args...)
	if err != nil {
		return nil, err
	}
	if int64(len(out)) > limit {
		return nil, fmt.Errorf("%s output exceeds %d bytes", name, limit)
	}
	return []byte(out), nil
}

type SystemRunner struct{}

// RunError preserves process exit information so callers can distinguish a
// command's documented empty-result status from an execution failure.
type RunError struct {
	Name     string
	Args     []string
	Output   string
	ExitCode int
	Err      error
}

func (e *RunError) Error() string {
	return fmt.Sprintf("%s %s: %v: %s", e.Name, strings.Join(e.Args, " "), e.Err, strings.TrimSpace(e.Output))
}

func (e *RunError) Unwrap() error { return e.Err }

func (SystemRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if finish := diagnostics.StartCommand(ctx, name, args); finish != nil {
		defer finish()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	// One buffer for both streams keeps them interleaved as CombinedOutput
	// did, bounded so no command can grow memory without limit.
	out := &boundedBuffer{limit: maxCommandOutput, stop: killOnLimit(cmd)}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	if out.stopped || errors.Is(err, errOutputLimit) {
		return "", &RunError{Name: name, Args: args, Output: diagnosticPrefix(out.String()), ExitCode: -1, Err: fmt.Errorf("output exceeds %d bytes; stopped", maxCommandOutput)}
	}
	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return out.String(), &RunError{Name: name, Args: args, Output: diagnosticPrefix(out.String()), ExitCode: exitCode, Err: err}
	}
	return out.String(), nil
}

func (SystemRunner) RunInteractive(ctx context.Context, name string, args ...string) error {
	if finish := diagnostics.StartCommand(ctx, name, args); finish != nil {
		defer finish()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	// Interactive commands (sudo pacman) get the terminal's own interrupt.
	// Cancellation sends the same signal rather than killing them, so they
	// can clean up (pacman releases its database lock).
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err, interrupted := runInteractive(cmd.Run)
	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if interrupted && ctx.Err() == nil {
			err = fmt.Errorf("%w (%w)", ErrSkippedByUser, err)
		}
		return &RunError{Name: name, Args: args, ExitCode: exitCode, Err: err}
	}
	return nil
}

var errOutputLimit = errors.New("output limit exceeded")

func (SystemRunner) RunOutput(ctx context.Context, limit int64, name string, args ...string) ([]byte, error) {
	if finish := diagnostics.StartCommand(ctx, name, args); finish != nil {
		defer finish()
	}
	if limit < 0 {
		return nil, fmt.Errorf("%s output exceeds %d bytes", name, limit)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	stdout := &boundedBuffer{limit: limit, stop: killOnLimit(cmd)}
	stderr := newTailBuffer(maxDiagnosticOutput)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if stdout.stopped || errors.Is(err, errOutputLimit) {
			return nil, fmt.Errorf("%s output exceeds %d bytes", name, limit)
		}
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return nil, &RunError{Name: name, Args: args, Output: stderr.String(), ExitCode: exitCode, Err: err}
	}
	return stdout.Bytes(), nil
}
