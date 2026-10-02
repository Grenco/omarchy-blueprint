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

func (p resourcesWorkflowProvider) BindReadSnapshot(c *observation.Cycle, snapshot workflow.ReadSnapshot) workflow.ReadProvider {
	source, ok := p.stateProvider.(*resourcesStateProvider)
	if !ok {
		return p.BindReadCycle(c)
	}
	// Construct a read-local source, never copy prepared Capture state. A nil
	// selected Machine inside this non-nil input explicitly freezes portable
	// defaults rather than falling back to a newly changed binding.
	snapshot = snapshot.Clone()
	opt := *source.opt
	worker := &resourcesStateProvider{deps: source.deps, opt: &opt, readSelection: &snapshot.Machine}
	return workflow.NarrowReadProvider(resourcesWorkflowProvider{restoreProviderAdapter{stateProvider: worker}})
}
