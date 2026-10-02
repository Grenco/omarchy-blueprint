package workflow

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/diagnostics"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
)

func TestReadCycleForwardedContextRetainsDiagnosticsAndCallerValues(t *testing.T) {
	t.Setenv("BLUEPRINT_DEBUG_OBSERVATION", "1")
	var out bytes.Buffer
	parent, finish := diagnostics.WithObservation(context.Background(), &out)
	r := &ReadCycle{observations: observation.New(parent), finishDiagnostics: finish}
	defer r.Close()
	type callerKey struct{}
	caller := context.WithValue(context.Background(), callerKey{}, "caller-value")
	ctx, stop := r.withContext(caller)
	defer stop()
	if ctx.Value(callerKey{}) != "caller-value" {
		t.Fatal("forwarded context lost caller values")
	}
	done := diagnostics.StartCommand(ctx, "git", []string{"status"})
	if done == nil {
		t.Fatal("forwarded provider/profile Git commands lost cycle diagnostics")
	}
	done()
	r.Close()
	if !strings.Contains(out.String(), "family=git verb=status calls=1") {
		t.Fatal("forwarded command missing from cycle summary")
	}
}
