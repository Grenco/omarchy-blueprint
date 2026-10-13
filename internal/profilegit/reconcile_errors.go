package profilegit

import "errors"

// Repair markers survive wrapping/joining without requiring a consumer to
// inspect or print transport errors, private paths or subprocess output.
var (
	ErrReconciliationRollbackFailed   = errors.New("HIGH SEVERITY: semantic merge rollback failed; repair repository before continuing")
	ErrReconciliationOutcomeUncertain = errors.New("reconciliation outcome uncertain; repair repository before continuing")
)
