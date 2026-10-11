package hooks

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// SyncDesired projects persistent semantic intent without consulting live state.
func (Provider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	s := syncengine.NewTargetSet("hooks")
	if !d.Manifest.Capture.Hooks {
		return s.Targets(), nil
	}
	for _, f := range d.Hooks.Items {
		if err := s.Add(f.Path, syncengine.StatePresent, f); err != nil {
			return nil, err
		}
	}
	for _, f := range d.Hooks.Absent {
		if err := s.Add(f.Path, syncengine.StateAbsent, f); err != nil {
			return nil, err
		}
	}
	return s.Targets(), nil
}
