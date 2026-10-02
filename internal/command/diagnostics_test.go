package command

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/diagnostics"
)

func TestRunnerDiagnosticsNeverRecordOutputBodies(t *testing.T) {
	t.Setenv("BLUEPRINT_DEBUG_OBSERVATION", "1")
	var log bytes.Buffer
	ctx, finish := diagnostics.WithObservation(context.Background(), &log)
	body := "private-output-body"
	out, e := (SystemRunner{}).RunOutput(ctx, 128, "sh", "-c", "printf 'private-output-body'")
	if e != nil || string(out) != body {
		t.Fatalf("runner output changed: %q %v", out, e)
	}
	finish()
	if strings.Contains(log.String(), body) || !strings.Contains(log.String(), "family=sh verb=other calls=1") {
		t.Fatal("unsafe/missing command diagnostics", log.String())
	}
}
