package diagnostics

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestObservationDiagnosticsDisabled(t *testing.T) {
	t.Setenv("BLUEPRINT_DEBUG_OBSERVATION", "")
	var out bytes.Buffer
	ctx := context.Background()
	got, finish := WithObservation(ctx, &out)
	if got != ctx {
		t.Fatal("disabled facility replaced context")
	}
	finish()
	if WithCollectorFrom(ctx, got) != ctx {
		t.Fatal("disabled collector forwarding replaced context")
	}
	if allocations := testing.AllocsPerRun(1000, func() { WithCollectorFrom(ctx, got) }); allocations != 0 {
		t.Fatal("disabled collector forwarding allocates", allocations)
	}
	args := []string{"--user", "show", "private.service"}
	if StartCommand(ctx, "systemctl", args) != nil {
		t.Fatal("disabled callback allocated")
	}
	if allocations := testing.AllocsPerRun(1000, func() { StartCommand(ctx, "systemctl", args) }); allocations != 0 {
		t.Fatal("disabled path allocates", allocations)
	}
	if out.Len() != 0 {
		t.Fatal("disabled facility wrote output")
	}
}

func TestObservationDiagnosticsRedactsPayloads(t *testing.T) {
	t.Setenv("BLUEPRINT_DEBUG_OBSERVATION", "1")
	t.Setenv("TEST_DIAGNOSTIC_SECRET", "environment-secret")
	var out bytes.Buffer
	ctx, finish := WithObservation(context.Background(), &out)
	for _, cmd := range [][]string{{"systemctl", "--user", "show", "private-unit.service"}, {"git", "-C", "/private/path", "status", "https://secret-token@example.test"}, {"/private/path/custom-secret-command", "secret-argument"}, {"sh", "-c", "printf output-secret"}} {
		done := StartCommand(ctx, cmd[0], cmd[1:])
		if done == nil {
			t.Fatal("enabled facility missing callback")
		}
		done()
	}
	done := StartObservation(ctx, "services")
	done()
	StartObservation(ctx, "private-provider")()
	finish()
	finish()
	text := out.String()
	for _, secret := range []string{"private-unit", "/private", "secret-", "environment-secret", "output-secret"} {
		if strings.Contains(text, secret) {
			t.Fatal("diagnostic exposed payload", secret)
		}
	}
	if !strings.Contains(text, "family=systemctl verb=show calls=1") || !strings.Contains(text, "provider=services observations=1") {
		t.Fatal("missing useful counts", text)
	}
	if strings.Count(text, "read-cycle total=") != 1 {
		t.Fatal("finish not idempotent", text)
	}
}
