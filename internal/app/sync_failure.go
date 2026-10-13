package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Only fixed, allowlisted text crosses the CLI boundary. Never render the
// original error: a wrapped/joined cause may contain paths or transport secrets.
type syncExecutionFailure struct {
	Code           string `json:"code"`
	Severity       string `json:"severity"`
	Message        string `json:"message"`
	Guidance       string `json:"guidance"`
	RepairRequired bool   `json:"repair_required"`
}

func (f syncExecutionFailure) Error() string { return f.Message + " " + f.Guidance }

func classifySyncExecutionFailure(err error) syncExecutionFailure {
	f := syncExecutionFailure{
		Code: "execution-stopped", Severity: "error",
		Message:  "Sync execution stopped; cursor was not advanced.",
		Guidance: "Inspect Profile Git and Sync before retrying; reviewed local history, published history or partial machine changes may remain.",
	}
	switch {
	case errors.Is(err, profilegit.ErrReconciliationPostCommitVerificationFailed):
		f.Code, f.Severity, f.RepairRequired = "repository-post-commit-verification-failed", "high", true
		f.Message = "HIGH SEVERITY: post-commit semantic verification failed; a commit may already exist and was not rolled back. The repository requires manual repair. Cursor was not advanced."
		f.Guidance = "Do not retry Sync. Manually repair the profile repository and verify its HEAD, index and worktree before a new Sync inspection."
	case errors.Is(err, profilegit.ErrReconciliationRollbackFailed):
		f.Code, f.Severity, f.RepairRequired = "repository-rollback-failed", "high", true
		f.Message = "HIGH SEVERITY: automatic semantic merge rollback failed; the repository requires manual repair. Cursor was not advanced."
		f.Guidance = "Do not retry Sync. Manually repair the profile repository and verify its HEAD, index and worktree before a new Sync inspection."
	case errors.Is(err, profilegit.ErrReconciliationOutcomeUncertain):
		f.Code, f.Severity, f.RepairRequired = "repository-outcome-uncertain", "high", true
		f.Message = "HIGH SEVERITY: the reconciliation outcome could not be verified; the repository requires manual repair. Cursor was not advanced."
		f.Guidance = "Do not retry Sync. Manually repair the profile repository and verify its HEAD, index and worktree before a new Sync inspection."
	case errors.Is(err, workflow.ErrScopedCaptureRepairRequired):
		f.Code, f.Severity, f.RepairRequired = "profile-recovery-failed", "high", true
		f.Message = "HIGH SEVERITY: scoped Capture recovery failed; the profile requires manual repair. Cursor was not advanced."
		f.Guidance = "Do not retry Sync. Manually repair and validate the profile, then inspect Profile Git and Sync before continuing."
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		f.Code = "execution-cancelled"
	}
	return f
}

func emitSyncExecutionFailure(deps Dependencies, opt *options, result workflow.SyncExecutionResult, err error) error {
	failure := classifySyncExecutionFailure(err)
	human := fmt.Sprintf("Sync failed [%s]\n%s\n%s\n", failure.Code, failure.Message, failure.Guidance)
	if emitErr := emit(deps.Out, opt.json, "sync apply", false, map[string]any{"execution": result, "failure": failure}, human); emitErr != nil {
		// stderr must retain the repair warning even when stdout is broken.
		// Output errors are not safe to print verbatim either.
		return fmt.Errorf("write Sync failure receipt failed: %w", failure)
	}
	return failure
}
