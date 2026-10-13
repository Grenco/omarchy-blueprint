package command

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func lowerCommandOutput(t *testing.T, limit int64) {
	t.Helper()
	previous := maxCommandOutput
	maxCommandOutput = limit
	t.Cleanup(func() { maxCommandOutput = previous })
}

// floodIgnoringPipe writes forever and ignores a closed pipe, as a runaway
// or hostile command might: only stopping it ends the call.
const floodIgnoringPipe = `trap '' PIPE; while :; do echo xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx 2>/dev/null; echo yyyyyyyy >&2; done`

func TestRunFailsAndStopsTheCommandPastTheOutputCap(t *testing.T) {
	lowerCommandOutput(t, 64<<10)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := SystemRunner{}.Run(ctx, "sh", "-c", floodIgnoringPipe)
	if ctx.Err() != nil {
		t.Fatal("Run kept the command running past the output cap")
	}
	var runErr *RunError
	if !errors.As(err, &runErr) || !strings.Contains(err.Error(), "output exceeds 65536 bytes") {
		t.Fatalf("err=%v", err)
	}
	// Parsed output is never handed back truncated.
	if out != "" {
		t.Fatalf("Run returned %d bytes of partial output", len(out))
	}
	if len(runErr.Output) > maxDiagnosticOutput+128 {
		t.Fatalf("error kept %d bytes of output", len(runErr.Output))
	}
}

func TestRunUnderTheCapKeepsCombinedOutputAndExitCode(t *testing.T) {
	lowerCommandOutput(t, 64<<10)
	out, err := SystemRunner{}.Run(context.Background(), "sh", "-c", "echo out; echo err >&2; exit 3")
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.ExitCode != 3 {
		t.Fatalf("err=%v", err)
	}
	if out != "out\nerr\n" || runErr.Output != out {
		t.Fatalf("out=%q runErr.Output=%q", out, runErr.Output)
	}
}

func TestRunOutputKeepsOnlyABoundedTailOfStderr(t *testing.T) {
	script := `head -c 4000000 /dev/zero | tr '\0' e >&2; echo LAST-LINE >&2; printf ok; exit 1`
	_, err := SystemRunner{}.RunOutput(context.Background(), 1024, "sh", "-c", script)
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.ExitCode != 1 {
		t.Fatalf("err=%v", err)
	}
	if len(runErr.Output) > maxDiagnosticOutput+128 {
		t.Fatalf("kept %d bytes of stderr", len(runErr.Output))
	}
	if !strings.Contains(runErr.Output, "LAST-LINE") || !strings.Contains(runErr.Output, "earlier bytes of output omitted") {
		t.Fatalf("tail or truncation note missing: %q…", runErr.Output[:80])
	}
}

func TestRunOutputLargeStderrDoesNotFailASuccessfulCommand(t *testing.T) {
	out, err := SystemRunner{}.RunOutput(context.Background(), 1024, "sh", "-c", `head -c 4000000 /dev/zero >&2; printf ok`)
	if err != nil || string(out) != "ok" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestRunOutputStopsACommandPastItsLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := SystemRunner{}.RunOutput(ctx, 4096, "sh", "-c", floodIgnoringPipe)
	if ctx.Err() != nil {
		t.Fatal("RunOutput kept the command running past its limit")
	}
	if err == nil || !strings.Contains(err.Error(), "output exceeds 4096 bytes") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunDetachedKeepsABoundedTail(t *testing.T) {
	script := `head -c 4000000 /dev/zero | tr '\0' e; echo LAST-LINE; exit 2`
	out, err := SystemRunner{}.RunDetached(context.Background(), "sh", "-c", script)
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.ExitCode != 2 {
		t.Fatalf("err=%v", err)
	}
	if len(out) > maxDiagnosticOutput+128 || !strings.Contains(out, "LAST-LINE") {
		t.Fatalf("kept %d bytes; tail present=%v", len(out), strings.Contains(out, "LAST-LINE"))
	}
}

func TestRunDetachedLargeOutputDoesNotFailASuccessfulCommand(t *testing.T) {
	if _, err := (SystemRunner{}).RunDetached(context.Background(), "sh", "-c", `head -c 4000000 /dev/zero`); err != nil {
		t.Fatal(err)
	}
}

func TestTailBufferKeepsTheLastBytes(t *testing.T) {
	b := newTailBuffer(8)
	for _, chunk := range []string{"abc", "defgh", "ij", "klmnopqrstu", "vw"} {
		if n, err := b.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("write %q: n=%d err=%v", chunk, n, err)
		}
	}
	if got := b.String(); !strings.HasPrefix(got, "[… 15 earlier bytes") || !strings.HasSuffix(got, "\npqrstuvw") {
		t.Fatalf("got %q", got)
	}
}
