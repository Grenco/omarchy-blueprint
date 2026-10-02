package services

import (
	"context"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type readView struct {
	source readSource
	slot   *observation.Slot[Observation]
}

var _ workflow.ReadRestoreProvider = (*readView)(nil)

func (p *Provider) BindReadCycle(c *observation.Cycle) workflow.ReadProvider {
	source := p.readSource()
	return &readView{source: source, slot: observation.NewSlot(c, source.observe, Observation.Clone)}
}
func (*readView) ID() string                   { return "services" }
func (*readView) CategoryEnabled() bool        { return true }
func (*readView) Captured(d profile.Data) bool { return d.Manifest.Capture.Services }
func (*readView) ValidateTarget(target string) (string, error) {
	return (&Provider{}).ValidateTarget(target)
}
func (*readView) ChangeTargetKey(change model.Change) (string, bool) {
	return (&Provider{}).ChangeTargetKey(change)
}
func (*readView) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return (&Provider{}).RestoreOperationTargetKeys(op)
}
func (v *readView) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	o, e := v.slot.Get(ctx)
	if e != nil {
		return nil, e
	}
	return v.source.diffObserved(ctx, d, o)
}
func (v *readView) InspectTargets(ctx context.Context, d profile.Data) ([]workflow.TargetInspection, error) {
	o, e := v.slot.Get(ctx)
	if e != nil {
		return nil, e
	}
	return v.source.targetsObserved(ctx, d, o)
}
func (v *readView) Plan(ctx context.Context, d profile.Data, info omarchy.Info, rc workflow.RestoreContext) (workflow.RestoreFragment, error) {
	if err := profile.Validate(d); err != nil {
		return workflow.RestoreFragment{}, err
	}
	selected, _, err := selectedServiceUnits(d, rc)
	if err != nil {
		return workflow.RestoreFragment{}, err
	}
	if len(selected) == 0 {
		return v.source.observedProvider(Observation{}).Plan(ctx, d, info, rc)
	}
	o, err := v.slot.Get(ctx)
	if err != nil {
		return workflow.RestoreFragment{}, err
	}
	return v.source.planObserved(ctx, d, o, info, rc)
}
func (s readSource) diffObserved(ctx context.Context, d profile.Data, o Observation) ([]model.Change, error) {
	return s.observedProvider(o).diff(ctx, d)
}
func (s readSource) targetsObserved(ctx context.Context, d profile.Data, o Observation) ([]workflow.TargetInspection, error) {
	return s.observedProvider(o).inspectTargets(ctx, d)
}
func (s readSource) planObserved(ctx context.Context, d profile.Data, o Observation, info omarchy.Info, rc workflow.RestoreContext) (workflow.RestoreFragment, error) {
	return s.observedProvider(o).Plan(ctx, d, info, rc)
}
