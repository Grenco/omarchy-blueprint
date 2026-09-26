package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// terminalRunner stands in for SystemRunner: buffered commands and
// terminal-attached commands are counted separately.
type terminalRunner struct {
	captureRunner
	buffered, interactive *int
}

func (r terminalRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "true" {
		*r.buffered++
		return "", nil
	}
	return r.captureRunner.Run(ctx, name, args...)
}

func (r terminalRunner) RunInteractive(context.Context, string, ...string) error {
	*r.interactive++
	return nil
}

func approvedRestoreSession(t *testing.T, provider captureTestProvider) (*Session, *int, *int) {
	t.Helper()
	session := newCaptureSession(t, profile.New("test", time.Now()))
	buffered, interactive := new(int), new(int)
	session.deps.Runner = terminalRunner{buffered: buffered, interactive: interactive}
	provider.order = &[]string{}
	session.SetProviders([]Provider{provider})
	return session, buffered, interactive
}

func TestApprovedRestoreRunsInteractiveOperationsAtTheTerminalOnce(t *testing.T) {
	session, buffered, interactive := approvedRestoreSession(t, captureTestProvider{id: "packages", interactive: true})
	approved, err := session.PlanRestore(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.ApplyApprovedRestore(context.Background(), "", nil, approved, true)
	if err != nil || !result.Applied || len(result.Execution.Completed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if *interactive != 1 || *buffered != 0 {
		t.Fatalf("interactive=%d buffered=%d; the operation must run through exactly one runner", *interactive, *buffered)
	}
}

func TestApprovedRestoreWithoutTerminalRefusesInteractiveBeforeMutation(t *testing.T) {
	session, buffered, interactive := approvedRestoreSession(t, captureTestProvider{id: "packages", interactive: true})
	approved, err := session.PlanRestore(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.ApplyApprovedRestore(context.Background(), "", nil, approved, false)
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") || result.Applied || *interactive+*buffered != 0 {
		t.Fatalf("result=%#v err=%v interactive=%d buffered=%d", result, err, *interactive, *buffered)
	}
}

func TestApprovedRestoreRefusesUnmetRequirementsBeforeMutation(t *testing.T) {
	session, buffered, interactive := approvedRestoreSession(t, captureTestProvider{id: "packages", requirement: true})
	approved, err := session.PlanRestore(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, terminal := range []bool{false, true} {
		result, err := session.ApplyApprovedRestore(context.Background(), "", nil, approved, terminal)
		var unmet *UnmetRequirementsError
		if !errors.As(err, &unmet) || !strings.Contains(err.Error(), "omarchy update") || result.Applied || *interactive+*buffered != 0 {
			t.Fatalf("terminal=%v result=%#v err=%v", terminal, result, err)
		}
	}
	if _, err := session.ApplyRestore(context.Background(), "", nil); !strings.Contains(errString(err), "omarchy update") {
		t.Fatalf("ApplyRestore must refuse unmet requirements too: %v", err)
	}
}

func TestApprovedRestoreRefusesAuthorityThatChangedAfterApproval(t *testing.T) {
	session, buffered, interactive := approvedRestoreSession(t, captureTestProvider{id: "packages", interactive: true})
	approved := model.RestorePlan{Operations: []model.Operation{{ID: "interactive", Provider: "packages", Resource: "semantic", Command: []string{"true"}}}}
	result, err := session.ApplyApprovedRestore(context.Background(), "", nil, approved, true)
	if !errors.Is(err, ErrRestorePlanChanged) || result.Applied || *interactive+*buffered != 0 {
		t.Fatalf("an interactive operation absent from the approved plan was authorized: result=%#v err=%v", result, err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
