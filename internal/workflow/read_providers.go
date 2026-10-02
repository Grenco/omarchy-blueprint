package workflow

import (
	"context"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
)

// NarrowReadProvider forwards only descriptive capabilities. Keeping optional
// scan interfaces on distinct wrappers preserves Status dispatch semantics.
func NarrowReadProvider(p ReadProvider) ReadProvider {
	base := &readForward{source: p}
	var result ReadProvider = base
	planner, canPlan := p.(ReadRestoreProvider)
	if canPlan {
		result = &readRestoreForward{readForward: base, planner: planner}
	}
	if scanner, ok := p.(scanProvider); ok {
		scan := &readScanForward{ReadProvider: base, scanner: scanner}
		if canPlan {
			return &readScanRestoreForward{readScanForward: scan, planner: planner}
		}
		return scan
	}
	if scanner, ok := p.(resourceProvider); ok {
		scan := &readResourceForward{ReadProvider: base, scanner: scanner}
		if canPlan {
			return &readResourceRestoreForward{readResourceForward: scan, planner: planner}
		}
		return scan
	}
	return result
}
func narrowReadProvider(p ReadProvider) ReadProvider { return NarrowReadProvider(p) }

type readForward struct{ source ReadProvider }

func (p *readForward) readCapabilitySource() ReadProvider         { return p.source }
func (p *readScanForward) readCapabilitySource() ReadProvider     { return p.ReadProvider }
func (p *readResourceForward) readCapabilitySource() ReadProvider { return p.ReadProvider }
func readCapabilitySource(p ReadProvider) ReadProvider {
	for {
		v, ok := p.(interface{ readCapabilitySource() ReadProvider })
		if !ok {
			return p
		}
		p = v.readCapabilitySource()
	}
}

func (p *readForward) ID() string                   { return p.source.ID() }
func (p *readForward) Captured(d profile.Data) bool { return p.source.Captured(profile.CloneData(d)) }
func (p *readForward) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	return p.source.Diff(ctx, profile.CloneData(d))
}
func (p *readForward) InspectTargets(ctx context.Context, d profile.Data) ([]TargetInspection, error) {
	return p.source.InspectTargets(ctx, profile.CloneData(d))
}
func (p *readForward) CategoryEnabled() bool {
	c, ok := p.source.(categoryProvider)
	return ok && c.CategoryEnabled()
}

func (p *readForward) categoryState() (bool, bool) {
	c, ok := p.source.(categoryProvider)
	if !ok {
		return false, false
	}
	return c.CategoryEnabled(), true
}
func (p *readScanForward) categoryState() (bool, bool)     { return readCategoryState(p.ReadProvider) }
func (p *readResourceForward) categoryState() (bool, bool) { return readCategoryState(p.ReadProvider) }

// Absence of CategoryEnabled differs from explicitly disabled: legacy
// providers still participate in aggregate Capture inspection but not the
// configurable category list. Narrow wrappers preserve both states.
func readCategoryState(p ReadProvider) (bool, bool) {
	if v, ok := p.(interface{ categoryState() (bool, bool) }); ok {
		return v.categoryState()
	}
	if c, ok := p.(categoryProvider); ok {
		return c.CategoryEnabled(), true
	}
	return false, false
}
func (p *readForward) ValidateTarget(target string) (string, error) {
	if v, ok := p.source.(targetValidator); ok {
		return v.ValidateTarget(target)
	}
	return target, nil
}
func (p *readForward) ChangeTargetKey(c model.Change) (string, bool) {
	if v, ok := p.source.(ChangeTargetResolver); ok {
		return v.ChangeTargetKey(c)
	}
	return "", false
}
func (p *readForward) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	if v, ok := p.source.(RestoreTargetResolver); ok {
		return v.RestoreOperationTargetKeys(op)
	}
	return nil, false
}

type readRestoreForward struct {
	*readForward
	planner ReadRestoreProvider
}

func (p *readRestoreForward) Plan(ctx context.Context, d profile.Data, info omarchy.Info, rc RestoreContext) (RestoreFragment, error) {
	return p.planner.Plan(ctx, profile.CloneData(d), info, rc)
}

type readScanForward struct {
	ReadProvider
	scanner scanProvider
}

func (p *readScanForward) DiffWithScan(ctx context.Context, d profile.Data) ([]model.Change, configprovider.ScanSummary, error) {
	return p.scanner.DiffWithScan(ctx, profile.CloneData(d))
}

// Explicit capability forwarding avoids embedding an authority-bearing type.
func (p *readScanForward) CategoryEnabled() bool {
	return p.ReadProvider.(categoryProvider).CategoryEnabled()
}
func (p *readScanForward) ValidateTarget(s string) (string, error) {
	return p.ReadProvider.(targetValidator).ValidateTarget(s)
}
func (p *readScanForward) ChangeTargetKey(c model.Change) (string, bool) {
	return p.ReadProvider.(ChangeTargetResolver).ChangeTargetKey(c)
}
func (p *readScanForward) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return p.ReadProvider.(RestoreTargetResolver).RestoreOperationTargetKeys(op)
}

type readScanRestoreForward struct {
	*readScanForward
	planner ReadRestoreProvider
}

func (p *readScanRestoreForward) Plan(ctx context.Context, d profile.Data, info omarchy.Info, rc RestoreContext) (RestoreFragment, error) {
	return p.planner.Plan(ctx, profile.CloneData(d), info, rc)
}

type readResourceForward struct {
	ReadProvider
	scanner resourceProvider
}

func (p *readResourceForward) DiffWithGitWorkingState(ctx context.Context, d profile.Data) ([]model.Change, map[string]resourcesprovider.GitWorkingSummary, error) {
	return p.scanner.DiffWithGitWorkingState(ctx, profile.CloneData(d))
}
func (p *readResourceForward) CategoryEnabled() bool {
	return p.ReadProvider.(categoryProvider).CategoryEnabled()
}
func (p *readResourceForward) ValidateTarget(s string) (string, error) {
	return p.ReadProvider.(targetValidator).ValidateTarget(s)
}
func (p *readResourceForward) ChangeTargetKey(c model.Change) (string, bool) {
	return p.ReadProvider.(ChangeTargetResolver).ChangeTargetKey(c)
}
func (p *readResourceForward) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return p.ReadProvider.(RestoreTargetResolver).RestoreOperationTargetKeys(op)
}

type readResourceRestoreForward struct {
	*readResourceForward
	planner ReadRestoreProvider
}

func (p *readResourceRestoreForward) Plan(ctx context.Context, d profile.Data, info omarchy.Info, rc RestoreContext) (RestoreFragment, error) {
	return p.planner.Plan(ctx, profile.CloneData(d), info, rc)
}

func readRestoreProvider(p ReadProvider) (ReadRestoreProvider, error) {
	v, ok := p.(ReadRestoreProvider)
	if !ok {
		return nil, fmt.Errorf("provider %s does not support restore", p.ID())
	}
	return v, nil
}
