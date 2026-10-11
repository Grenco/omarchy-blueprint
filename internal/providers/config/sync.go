package config

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// SyncDesired projects persistent semantic intent without consulting live state.
func (Provider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	s := syncengine.NewTargetSet("config")
	if !d.Manifest.Capture.Config {
		return s.Targets(), nil
	}
	for _, f := range d.Config.Files {
		f.ID = ""
		if err := s.Add(f.Path, syncengine.StatePresent, f); err != nil {
			return nil, err
		}
	}
	for _, f := range d.Config.Deletes {
		if err := s.Add(f.Path, syncengine.StateAbsent, f); err != nil {
			return nil, err
		}
	}
	return s.Targets(), nil
}
