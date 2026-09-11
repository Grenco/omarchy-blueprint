package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

type OutputRunner interface {
	RunOutput(ctx context.Context, limit int64, name string, args ...string) ([]byte, error)
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

var errOutputLimit = errors.New("output limit exceeded")

type boundedBuffer struct {
	data  bytes.Buffer
	limit int64
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if int64(b.data.Len()+len(p)) > b.limit {
		return 0, errOutputLimit
	}
	return b.data.Write(p)
}

func (SystemRunner) RunOutput(ctx context.Context, limit int64, name string, args ...string) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("%s output exceeds %d bytes", name, limit)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	stdout := &boundedBuffer{limit: limit}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, errOutputLimit) {
			return nil, fmt.Errorf("%s output exceeds %d bytes", name, limit)
		}
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return nil, &RunError{Name: name, Args: args, Output: stderr.String(), ExitCode: exitCode, Err: err}
	}
	return stdout.data.Bytes(), nil
}
