package packages

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// SyncDesired projects committed intent without probing pacman or the current
// Omarchy catalogue. Historical interpretation must not depend on today's host.
func (Provider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	s := syncengine.NewTargetSet("packages")
	// Packages predate capture metadata; their stored declarations are intent
	// regardless of this flag, just as the existing workflow adapter reports.
	p := profile.ClonePackages(d.Packages)
	excluded := map[string]bool{}
	for _, key := range p.Excluded {
		excluded[key] = true
	}
	for _, key := range p.MachineSpecific {
		excluded[key] = true
	}
	add := func(key string, kind syncengine.StateKind, value any) error {
		if excluded[key] {
			return nil
		}
		return s.Add(key, kind, value)
	}
	for _, name := range p.Official {
		if err := add("official:"+name, syncengine.StatePresent, true); err != nil {
			return nil, err
		}
	}
	for _, name := range p.AUR {
		if err := add("aur:"+name, syncengine.StatePresent, true); err != nil {
			return nil, err
		}
	}
	for id, tool := range p.Mise {
		normalized, err := NormalizeMiseTool(id, map[string]any(tool))
		if err != nil {
			return nil, err
		}
		if err := add("mise:"+id, syncengine.StatePresent, normalized); err != nil {
			return nil, err
		}
	}
	for _, a := range p.Absent {
		if err := add(a.Ref, syncengine.StateAbsent, a.Mise); err != nil {
			return nil, err
		}
	}
	if p.Preinstalls.Managed {
		kind := syncengine.StatePresent
		if p.Preinstalls.RemovedAll {
			kind = syncengine.StateAbsent
		}
		if err := s.Add("preinstalls", kind, !p.Preinstalls.RemovedAll); err != nil {
			return nil, err
		}
	}
	for id, present := range p.Preinstalls.Items {
		kind := syncengine.StatePresent
		if !present {
			kind = syncengine.StateAbsent
		}
		if err := add("preinstall:"+id, kind, present); err != nil {
			return nil, err
		}
	}
	return s.Targets(), nil
}
