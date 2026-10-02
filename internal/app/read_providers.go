package app

import (
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func (p restoreProviderAdapter) BindReadCycle(c *observation.Cycle) workflow.ReadProvider {
	if binder, ok := p.stateProvider.(workflow.ReadCycleBinder); ok {
		return binder.BindReadCycle(c)
	}
	return workflow.NarrowReadProvider(p)
}

// These wrappers must preserve specialized Diff capabilities rather than
// forwarding through their embedded generic authoritative adapter.
func (p configWorkflowProvider) BindReadCycle(*observation.Cycle) workflow.ReadProvider {
	return workflow.NarrowReadProvider(p)
}
func (p resourcesWorkflowProvider) BindReadCycle(*observation.Cycle) workflow.ReadProvider {
	return workflow.NarrowReadProvider(p)
}
