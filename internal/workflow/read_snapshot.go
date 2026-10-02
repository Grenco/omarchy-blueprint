package workflow

import (
	"context"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
)

type readConfig struct {
	deps       Dependencies
	opts       Options
	providers  []Provider
	profileGit profilegit.Service
	finalize   func(context.Context, profile.Data, []string, *model.RestorePlan, policy.RestoreOptions) error
}

// ReadSnapshot is descriptive desired state, never mutation authority.
// Copies may be retained by presentation after the owning cycle is closed.
type ReadSnapshot struct {
	Profile profile.Data
	Machine machine.Selection
}

func (s ReadSnapshot) Clone() ReadSnapshot {
	return ReadSnapshot{Profile: profile.CloneData(s.Profile), Machine: cloneMachine(s.Machine)}
}

// ReadSnapshotBinder is optional for adapters whose read interpretation needs
// the captured machine selection as well as provider observation slots.
type ReadSnapshotBinder interface {
	BindReadSnapshot(*observation.Cycle, ReadSnapshot) ReadProvider
}

func (r *ReadCycle) Snapshot(ctx context.Context) (ReadSnapshot, error) {
	if err := r.check(ctx); err != nil {
		return ReadSnapshot{}, err
	}
	copy := (ReadSnapshot{Profile: r.profile, Machine: r.machine}).Clone()
	if err := r.check(ctx); err != nil {
		return ReadSnapshot{}, err
	}
	return copy, nil
}

// loadReadSnapshot loads desired state privately; it never publishes to a
// Session or grants a read cycle mutation authority.
func loadReadSnapshot(ctx context.Context, cfg readConfig) (profile.Data, machine.Selection, error) {
	var empty profile.Data
	var selection machine.Selection
	if err := ctx.Err(); err != nil {
		return empty, selection, err
	}
	dir, err := machine.CanonicalProfileRoot(cfg.opts.ProfileDir)
	if err != nil {
		return empty, selection, err
	}
	data, err := profile.Load(dir)
	if err != nil {
		return empty, selection, err
	}
	if err := validateLoadedPolicyTargets(cfg.providers, data); err != nil {
		return empty, selection, err
	}
	state, err := cfg.deps.StateHome()
	if err != nil {
		return empty, selection, err
	}
	bound, err := (machine.BindingStore{StateHome: state}).Load(dir)
	if err != nil {
		return empty, selection, err
	}
	selection, err = machine.Select(cfg.opts.ExplicitMachine, bound, data.Machines.Items)
	if err != nil {
		return empty, selection, err
	}
	if err := ctx.Err(); err != nil {
		return empty, selection, err
	}
	return profile.CloneData(data), cloneMachine(selection), nil
}

func cloneMachine(m machine.Selection) machine.Selection {
	if m.Machine != nil {
		d := profile.CloneData(profile.Data{Machines: profile.Machines{Items: []profile.Machine{*m.Machine}}})
		m.Machine = &d.Machines.Items[0]
	}
	return m
}
