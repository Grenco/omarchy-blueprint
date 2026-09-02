package command

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
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
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return string(out), &RunError{Name: name, Args: args, Output: string(out), ExitCode: exitCode, Err: err}
	}
	return string(out), nil
}
