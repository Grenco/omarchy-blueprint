package services

import (
	"context"
	"fmt"
	"slices"
)

// Observation contains systemd facts and resolved source roots, not filesystem
// ownership or mutation authority. Artifact/fingerprint checks remain fresh.
type Observation struct {
	Roots      Roots
	Units      []ObservedUnit
	inspectErr error
}

func (o Observation) Clone() Observation {
	o.Units = slices.Clone(o.Units)
	for i := range o.Units {
		o.Units[i].DropInPaths = slices.Clone(o.Units[i].DropInPaths)
		o.Units[i].RelatedUnits = slices.Clone(o.Units[i].RelatedUnits)
		o.Units[i].TriggeredBy = slices.Clone(o.Units[i].TriggeredBy)
	}
	return o
}

// readSource deliberately excludes prepared Capture transaction state.
type readSource struct {
	Systemd      Systemd
	Roots        Roots
	ResolveRoots func() (Roots, error)
	ProfileDir   string
}

func (p *Provider) readSource() readSource {
	return readSource{Systemd: p.Systemd, Roots: p.Roots, ResolveRoots: p.ResolveRoots, ProfileDir: p.ProfileDir}
}

func (s readSource) localProvider() (*Provider, error) {
	roots := s.Roots
	if s.ResolveRoots != nil {
		var err error
		roots, err = s.ResolveRoots()
		if err != nil {
			return nil, fmt.Errorf("resolve user-service source roots: %w", err)
		}
	}
	return &Provider{Systemd: s.Systemd, Roots: roots, ProfileDir: s.ProfileDir}, nil
}

func (s readSource) observe(ctx context.Context) (Observation, error) {
	p, err := s.localProvider()
	if err != nil {
		return Observation{}, err
	}
	if p.Systemd == nil {
		return Observation{}, fmt.Errorf("Services needs a user systemd inspection source")
	}
	units, err := p.Systemd.InspectUserUnits(ctx)
	if canceled := ctx.Err(); canceled != nil {
		return Observation{}, canceled
	}
	// Keep manager failure as raw facts alongside the resolved roots. Different
	// projections apply their existing policy-dependent failure behavior; never
	// cache a manufactured fallback inventory or turn failure into absence.
	return (Observation{Roots: p.Roots, Units: units, inspectErr: err}).Clone(), nil
}

type observedSystemd struct {
	Systemd
	facts Observation
}

func (s observedSystemd) InspectUserUnits(ctx context.Context) ([]ObservedUnit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.facts.Clone().Units, s.facts.inspectErr
}
func (s readSource) observedProvider(o Observation) *Provider {
	return &Provider{Systemd: observedSystemd{Systemd: s.Systemd, facts: o}, Roots: o.Roots, ProfileDir: s.ProfileDir}
}
