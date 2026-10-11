package services

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"sort"
)

// NormalizeSyncUnit retains persistent intent and owned artifact provenance.
// Runtime active state matters only when explicitly selected as restore intent.
func NormalizeSyncUnit(unit profile.ServiceUnit) profile.ServiceUnit {
	d := profile.CloneData(profile.Data{Services: profile.Services{Units: []profile.ServiceUnit{unit}}})
	u := d.Services.Units[0]
	if u.ActivationPreference == "" {
		u.ActivationPreference = profile.ServiceActivationPersistentOnly
	}
	if u.ActivationPreference == profile.ServiceActivationPersistentOnly {
		u.ObservedActive = false
	}
	sort.Slice(u.DropIns, func(i, j int) bool { return u.DropIns[i].Path < u.DropIns[j].Path })
	for i := range u.Instances {
		x := &u.Instances[i]
		if x.ActivationPreference == "" {
			x.ActivationPreference = profile.ServiceActivationPersistentOnly
		}
		if x.ActivationPreference == profile.ServiceActivationPersistentOnly {
			x.ObservedActive = false
		}
		sort.Slice(x.DropIns, func(i, j int) bool { return x.DropIns[i].Path < x.DropIns[j].Path })
	}
	sort.Slice(u.Instances, func(i, j int) bool { return u.Instances[i].Name < u.Instances[j].Name })
	return u
}

func (*Provider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	s := syncengine.NewTargetSet("services")
	if !d.Manifest.Capture.Services {
		return s.Targets(), nil
	}
	for _, u := range d.Services.Units {
		kind := syncengine.StatePresent
		if u.Presence == profile.ServiceAbsent {
			kind = syncengine.StateAbsent
		}
		if err := s.Add(u.Name, kind, NormalizeSyncUnit(u)); err != nil {
			return nil, err
		}
	}
	return s.Targets(), nil
}
