package services

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"os"
	"path/filepath"
)

func (v *readView) SyncObserve(ctx context.Context, hints profile.Data, histories []profile.Data) (workflow.SyncLiveObservation, error) {
	o, err := v.slot.Get(ctx)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	if o.inspectErr != nil {
		return workflow.SyncLiveObservation{}, fmt.Errorf("sync: user service manager unavailable")
	}
	p := v.source.observedProvider(o)
	// All historical identities participate; current-only candidates still need
	// reviewed adoption and must never become implicitly owned Sync targets.
	union := profile.CloneData(hints)
	seen := map[string]bool{}
	for _, u := range union.Services.Units {
		seen[u.Name] = true
	}
	for _, d := range histories {
		for _, u := range d.Services.Units {
			if !seen[u.Name] {
				seen[u.Name] = true
				union.Services.Units = append(union.Services.Units, u)
			}
		}
	}
	inspections, err := p.inspectTargets(ctx, union)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	candidates, err := p.discovered(ctx, union.Services)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	byName := map[string]Candidate{}
	for _, c := range candidates {
		byName[c.Unit.Name] = c
	}
	byUnit := map[string]ObservedUnit{}
	for _, u := range o.Units {
		byUnit[u.Name] = u
	}
	states := make([]map[string]syncengine.State, len(histories))
	for i, d := range histories {
		targets, err := p.SyncDesired(d)
		if err != nil {
			return workflow.SyncLiveObservation{}, err
		}
		states[i] = map[string]syncengine.State{}
		for _, x := range targets {
			states[i][x.Key.Target] = x.State
		}
	}
	targetByName := map[string]workflow.TargetInspection{}
	for _, x := range inspections {
		targetByName[x.Key] = x
	}
	result := workflow.SyncLiveObservation{}
	for _, saved := range union.Services.Units {
		if err := ctx.Err(); err != nil {
			return workflow.SyncLiveObservation{}, err
		}
		candidate, ok := byName[saved.Name]
		if !ok {
			return workflow.SyncLiveObservation{}, fmt.Errorf("sync: managed service candidate is unresolved")
		}
		inspection, ok := targetByName[saved.Name]
		if !ok {
			return workflow.SyncLiveObservation{}, fmt.Errorf("sync: service policy identity is unresolved")
		}
		key := syncengine.TargetKey{Category: "services", Target: saved.Name}
		var state syncengine.State
		if candidate.Missing {
			alternatives := make([]syncengine.State, len(states))
			for i, m := range states {
				if s, ok := m[saved.Name]; ok {
					alternatives[i] = s
				} else {
					alternatives[i] = syncengine.State{Kind: syncengine.StateUnmanaged}
				}
			}
			state = syncengine.ObservedMissing(true, alternatives)
		} else {
			if !candidate.Eligible {
				return workflow.SyncLiveObservation{}, fmt.Errorf("sync: managed service cannot be normalized safely")
			}
			// Pure projection reads authored content but never stages Capture files.
			projected, _, err := p.projectServiceUnit(candidate, saved, true, true)
			if err != nil {
				return workflow.SyncLiveObservation{}, fmt.Errorf("sync: managed service artifacts are unreadable")
			}
			for i := range projected.Instances {
				instance := &projected.Instances[i]
				actual, found := byUnit[instance.Name]
				if !found {
					instance.Presence = profile.ServiceAbsent
					instance.StartIntent = profile.ServiceStartNotManaged
					instance.ObservedActive = false
				} else {
					if instance.Presence == profile.ServiceAbsent && actual.StartIntent == profile.ServiceStartDisabled {
						instance.StartIntent = profile.ServiceStartNotManaged
					} else {
						instance.Presence = profile.ServicePresent
						instance.StartIntent = actual.StartIntent
						instance.ObservedActive = actual.ObservedActive
					}
				}
				for j := range instance.DropIns {
					a := &instance.DropIns[j]
					path := filepath.Join(p.Roots.UserConfigDir, instance.Name+".d", filepath.Base(a.Path))
					observed, err := observeSyncDropIn(*a, path)
					if err != nil {
						return workflow.SyncLiveObservation{}, err
					}
					*a = observed
				}
				if instance.Mask != nil {
					presence := profile.ServiceAbsent
					if userMask(instance.Name, p.Roots) {
						presence = profile.ServicePresent
					}
					instance.Mask = &profile.ServiceMask{Presence: presence}
				}
			}
			fingerprint, err := syncengine.Fingerprint(NormalizeSyncUnit(projected))
			if err != nil {
				return workflow.SyncLiveObservation{}, err
			}
			state = syncengine.State{Kind: syncengine.StatePresent, Fingerprint: fingerprint}
		}
		result.Targets = append(result.Targets, syncengine.DesiredTarget{Key: key, State: state})
		result.Inspections = append(result.Inspections, inspection)
	}
	return result, nil
}

func observeSyncDropIn(prior profile.ServiceArtifact, path string) (profile.ServiceArtifact, error) {
	// readServiceFile intentionally redacts filesystem errors. Establish
	// absence separately without mistaking a dangling symlink for absence.
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
			info, err := os.Lstat(parent)
			if err == nil {
				if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
					return profile.ServiceArtifact{}, fmt.Errorf("sync: instance artifact parent is unsafe")
				}
			} else if !os.IsNotExist(err) {
				return profile.ServiceArtifact{}, fmt.Errorf("sync: instance artifact parent is unreadable")
			}
			if parent == filepath.Dir(parent) {
				break
			}
		}
		prior.Presence = profile.ServiceAbsent
		return prior, nil
	}
	file, err := readServiceFile(path)
	if err == nil {
		prior.Presence, prior.Hash, prior.Mode = profile.ServicePresent, file.hash, file.mode
		return prior, nil
	}
	return profile.ServiceArtifact{}, fmt.Errorf("sync: instance artifact cannot be inspected safely")
}
