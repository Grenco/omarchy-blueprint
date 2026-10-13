package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type syncFailureBrokenWriter struct{}

func (syncFailureBrokenWriter) Write([]byte) (int, error) {
	return 0, errors.New("private-writer-error /private/report")
}

func TestSyncFailureReportWriteErrorPreservesRepairGuidance(t *testing.T) {
	err := emitSyncExecutionFailure(Dependencies{Out: syncFailureBrokenWriter{}}, &options{json: true}, workflow.SyncExecutionResult{}, profilegit.ErrReconciliationRollbackFailed)
	var failure syncExecutionFailure
	if !errors.As(err, &failure) || !failure.RepairRequired || !strings.Contains(err.Error(), "Do not retry Sync") {
		t.Fatal("failed output suppressed repair instructions")
	}
	if strings.Contains(err.Error(), "private-writer-error") || strings.Contains(err.Error(), "/private/report") {
		t.Fatal("raw output error escaped safe failure boundary")
	}
}

func TestSyncFailureRepairMarkersSurviveWrappingWithoutLeakingCauses(t *testing.T) {
	for _, tc := range []struct {
		cause  error
		code   string
		repair bool
	}{
		{profilegit.ErrReconciliationPostCommitVerificationFailed, "repository-post-commit-verification-failed", true},
		{profilegit.ErrReconciliationRollbackFailed, "repository-rollback-failed", true},
		{profilegit.ErrReconciliationOutcomeUncertain, "repository-outcome-uncertain", true},
		{workflow.ErrScopedCaptureRepairRequired, "profile-recovery-failed", true},
		{context.Canceled, "execution-cancelled", false},
		{errors.New("HIGH SEVERITY: semantic merge rollback failed"), "execution-stopped", false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			var out bytes.Buffer
			cause := fmt.Errorf("private-cause: %w", errors.Join(context.Canceled, tc.cause))
			// The ordinary spoof case must not be recognized as a repair marker.
			if tc.code == "execution-stopped" {
				cause = fmt.Errorf("private-cause: %w", tc.cause)
			}
			err := emitSyncExecutionFailure(Dependencies{Out: &out}, &options{json: true}, workflow.SyncExecutionResult{}, cause)
			var envelope struct {
				Data struct {
					Failure syncExecutionFailure `json:"failure"`
				} `json:"data"`
			}
			if err == nil || json.Unmarshal(out.Bytes(), &envelope) != nil {
				t.Fatal("missing failure receipt")
			}
			f := envelope.Data.Failure
			if f.Code != tc.code || f.RepairRequired != tc.repair {
				t.Fatalf("classification lost: %#v", f)
			}
			if tc.repair && (f.Severity != "high" || !strings.Contains(f.Guidance, "Do not retry Sync")) {
				t.Fatal("cancellation hid repair guidance")
			}
			if strings.Contains(out.String()+err.Error(), "private-cause") {
				t.Fatal("wrapped cause exposed")
			}
		})
	}
}
