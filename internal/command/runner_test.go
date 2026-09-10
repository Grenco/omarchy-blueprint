package command

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunOutputSeparatesStderrAndPreservesBytes(t *testing.T) {
	out, err := RunOutput(context.Background(), SystemRunner{}, 1024, "sh", "-c", "printf '\\001\\000\\377'; printf 'warning' >&2")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, []byte{1, 0, 255}) {
		t.Fatalf("out=%v", out)
	}
}

func TestRunOutputRejectsLimit(t *testing.T) {
	_, err := RunOutput(context.Background(), SystemRunner{}, 3, "sh", "-c", "printf 'abcd'")
	if err == nil || !strings.Contains(err.Error(), "output exceeds 3 bytes") {
		t.Fatalf("err=%v", err)
	}
}

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
