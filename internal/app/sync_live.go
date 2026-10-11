package app

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	packagesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"sort"
	"strings"
)

func normalizeSyncPackages(d profile.Data, catalogue map[string]bool) profile.Data {
	d = profile.CloneData(d)
	d.Packages = packagesprovider.CanonicalizePreinstallOwnership(d.Packages, catalogue)
	canonicalRefs := func(refs []string) []string {
		for i, ref := range refs {
			kind, id, ok := strings.Cut(ref, ":")
			if ok && (kind == "official" || kind == "aur") {
				if _, found := catalogue[id]; found {
					refs[i] = "preinstall:" + id
				}
			}
		}
		return refs
	}
	d.Packages.Excluded = canonicalRefs(d.Packages.Excluded)
	d.Packages.MachineSpecific = canonicalRefs(d.Packages.MachineSpecific)
	migrateLegacyPreinstallPolicyAliases(&d.Policy, catalogue)
	for i := range d.Machines.Items {
		migrateLegacyPreinstallPolicyAliases(&d.Machines.Items[i].Policy, catalogue)
	}
	return d
}

func (v *packagesReadView) NormalizeSyncProfile(ctx context.Context, d profile.Data) (profile.Data, error) {
	facts, err := v.slot.Get(ctx)
	if err != nil {
		return profile.Data{}, err
	}
	return normalizeSyncPackages(d, facts.observed.Packages().Preinstalls.Items), nil
}

// syncLiveFacts combines provider-normalized values with provider-reported
// physical presence. It never reconstructs meaning from capture fingerprints.
// Missing state uses tombstone provenance only when physical absence is proven.
func syncLiveFacts(category string, current []syncengine.DesiredTarget, inspections []workflow.TargetInspection, histories [][]syncengine.DesiredTarget) (workflow.SyncLiveObservation, error) {
	values := map[string]syncengine.DesiredTarget{}
	keys := map[string]bool{}
	for _, x := range current {
		values[x.Key.Target] = x
		keys[x.Key.Target] = true
	}
	observed := map[string]workflow.TargetInspection{}
	for _, x := range inspections {
		if _, ok := observed[x.Key]; !ok {
			observed[x.Key] = x
		}
	}
	prior := make([]map[string]syncengine.State, len(histories))
	for i, items := range histories {
		prior[i] = map[string]syncengine.State{}
		for _, x := range items {
			prior[i][x.Key.Target] = x.State
			keys[x.Key.Target] = true
		}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	result := workflow.SyncLiveObservation{}
	for _, key := range ordered {
		target, ok := observed[key]
		if !ok {
			return result, fmt.Errorf("sync: normalized live target lacks provider inspection")
		}
		value, exists := values[key]
		switch target.Current {
		case workflow.TargetUnknown:
			return result, fmt.Errorf("sync: physical target state is unknown")
		case workflow.TargetAbsent:
			alternatives := make([]syncengine.State, len(prior))
			for i, states := range prior {
				if x, ok := states[key]; ok {
					alternatives[i] = x
				} else {
					alternatives[i] = syncengine.State{Kind: syncengine.StateUnmanaged}
				}
			}
			missing := syncengine.ObservedMissing(target.Capabilities.SupportsDesiredAbsence, alternatives)
			if exists && value.State.Kind == syncengine.StateAbsent && missing.Kind == syncengine.StateUnmanaged && target.Capabilities.RecordsNewAbsence {
				missing = value.State
			}
			value = syncengine.DesiredTarget{Key: syncengine.TargetKey{Category: category, Target: key}, State: missing}
		case workflow.TargetPresent:
			if !exists {
				if target.Capabilities.NoActionableUpdate || target.Capabilities.DropsDesiredWhenIneligible {
					value = syncengine.DesiredTarget{Key: syncengine.TargetKey{Category: category, Target: key}, State: syncengine.State{Kind: syncengine.StateUnmanaged}}
				} else {
					return result, fmt.Errorf("sync: present target has no normalized live value")
				}
			}
		default:
			return result, fmt.Errorf("sync: invalid physical target state")
		}
		result.Targets = append(result.Targets, value)
		result.Inspections = append(result.Inspections, target)
	}
	return result, nil
}

func syncHistories(d []profile.Data, projection func(profile.Data) ([]syncengine.DesiredTarget, error)) ([][]syncengine.DesiredTarget, error) {
	out := make([][]syncengine.DesiredTarget, len(d))
	for i, p := range d {
		var err error
		out[i], err = projection(p)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (v *packagesReadView) SyncObserve(ctx context.Context, hints profile.Data, histories []profile.Data) (workflow.SyncLiveObservation, error) {
	facts, err := v.slot.Get(ctx)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	return syncPackagesObserved(hints, histories, facts.observed.Packages())
}
func syncPackagesObserved(hints profile.Data, histories []profile.Data, current profile.Packages) (workflow.SyncLiveObservation, error) {
	hints = normalizeSyncPackages(hints, current.Preinstalls.Items)
	normalizedHistories := make([]profile.Data, len(histories))
	for i, d := range histories {
		normalizedHistories[i] = normalizeSyncPackages(d, current.Preinstalls.Items)
	}
	histories = normalizedHistories
	p := packagesStateProvider{}
	currentData := profile.CloneData(hints)
	currentData.Packages = current
	currentData.Manifest.Capture.Packages = true
	normalized, err := p.SyncDesired(currentData)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	inspections, err := targetsFromPackages(hints, current)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	var evidence []workflow.SyncDesiredInspection
	for _, d := range histories {
		part, err := targetsFromPackages(d, current)
		if err != nil {
			return workflow.SyncLiveObservation{}, err
		}
		inspections = append(inspections, part...)
		desired, err := p.SyncDesired(d)
		if err != nil {
			return workflow.SyncLiveObservation{}, err
		}
		bound, err := workflow.BindSyncDesiredInspections(desired, part)
		if err != nil {
			return workflow.SyncLiveObservation{}, err
		}
		evidence = append(evidence, bound...)
	}
	variants, err := syncHistories(histories, p.SyncDesired)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	if current.OriginUnavailable {
		return workflow.SyncLiveObservation{}, fmt.Errorf("sync: package origin cannot be established")
	}
	result, err := syncLiveFacts("packages", normalized, inspections, variants)
	result.DesiredInspections = evidence
	return result, err
}
