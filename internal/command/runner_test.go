package command

import (
	"context"
	"errors"
	"testing"
)

func TestSystemRunnerPreservesExitCode(t *testing.T) {
	_, err := (SystemRunner{}).Run(context.Background(), "sh", "-c", "exit 7")
	var runErr *RunError
	if !errors.As(err, &runErr) {
		t.Fatalf("expected RunError, got %T: %v", err, err)
	}
	if runErr.ExitCode != 7 {
		t.Fatalf("exit code = %d", runErr.ExitCode)
	}
}
