package app

import (
	"context"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	packagesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type packagesReadFacts struct {
	detector packagesprovider.Provider
	observed packagesprovider.Observation
}

func (f packagesReadFacts) clone() packagesReadFacts { f.observed = f.observed.Clone(); return f }

type packagesReadView struct {
	slot *observation.Slot[packagesReadFacts]
}

var _ workflow.ReadRestoreProvider = (*packagesReadView)(nil)

func (p packagesStateProvider) BindReadCycle(c *observation.Cycle) workflow.ReadProvider {
	return &packagesReadView{slot: observation.NewSlot(c, func(ctx context.Context) (packagesReadFacts, error) {
		detector, err := p.provider()
		if err != nil {
			return packagesReadFacts{}, err
		}
		observed, err := detector.Observe(ctx)
		return packagesReadFacts{detector: detector, observed: observed}, err
	}, packagesReadFacts.clone)}
}
func (*packagesReadView) ID() string                 { return "packages" }
func (*packagesReadView) CategoryEnabled() bool      { return true }
func (*packagesReadView) Captured(profile.Data) bool { return true }
func (*packagesReadView) ValidateTarget(target string) (string, error) {
	return (packagesStateProvider{}).ValidateTarget(target)
}
func (*packagesReadView) ChangeTargetKey(change model.Change) (string, bool) {
	return (packagesStateProvider{}).ChangeTargetKey(change)
}
func (*packagesReadView) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return (packagesStateProvider{}).RestoreOperationTargetKeys(op)
}
func (v *packagesReadView) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	f, e := v.slot.Get(ctx)
	if e != nil {
		return nil, e
	}
	return packagesprovider.Diff(d.Packages, f.observed.Packages()), nil
}
func (v *packagesReadView) InspectTargets(ctx context.Context, d profile.Data) ([]workflow.TargetInspection, error) {
	f, e := v.slot.Get(ctx)
	if e != nil {
		return nil, e
	}
	return targetsFromPackages(d, f.observed.Packages())
}
func (v *packagesReadView) Plan(ctx context.Context, d profile.Data, info omarchy.Info, rc workflow.RestoreContext) (workflow.RestoreFragment, error) {
	f, e := v.slot.Get(ctx)
	if e != nil {
		return workflow.RestoreFragment{}, e
	}
	return planFromPackages(d, info, rc, f.detector, f.observed.Packages())
}
