package screens

import (
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func initialPolicyScope(session *workflow.Session) workflow.PolicyScope {
	if session == nil {
		return workflow.PolicyScope{}
	}
	return workflow.PolicyScope{Machine: session.Machine().Name}
}

func policyScopeLabel(scope workflow.PolicyScope) string {
	if scope.Machine != "" {
		return "Machine: " + components.DisplayText(scope.Machine)
	}
	return "Profile defaults"
}

// togglePolicyScope makes the portable profile scope directly reachable
// without changing which machine is active for Capture or Restore. From the
// profile scope it returns to the active machine, when one exists.
func togglePolicyScope(session *workflow.Session, scope workflow.PolicyScope) workflow.PolicyScope {
	if scope.Machine != "" || session == nil {
		return workflow.PolicyScope{}
	}
	return workflow.PolicyScope{Machine: session.Machine().Name}
}
