package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestObservationDiagnosticsPreservesJSONStdout(t *testing.T) {
	t.Setenv("BLUEPRINT_DEBUG_OBSERVATION", "1")
	dir, deps := configSandbox(t)
	var stdout, stderr bytes.Buffer
	deps.Out = &stdout
	deps.Err = &stderr
	deps.MiseGlobalConfig = func() (string, error) { return "", nil }
	code := Execute(context.Background(), []string{"--profile", dir, "--json", "status", "packages"}, deps)
	if code != 0 && code != 2 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatal("diagnostics corrupted JSON stdout")
	}
	if !strings.Contains(stderr.String(), "provider=packages observations=1") {
		t.Fatal("missing stderr diagnostics", stderr.String())
	}
	if strings.Contains(stderr.String(), dir) {
		t.Fatal("diagnostics exposed profile path")
	}
}
