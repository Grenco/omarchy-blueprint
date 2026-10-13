package shell

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// SyncDesired projects persistent semantic intent without consulting live state.
func (Provider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	s := syncengine.NewTargetSet("shell")
	if !d.Manifest.Capture.Shell || d.Shell.Hash == "" {
		return s.Targets(), nil
	}
	if err := s.Add("state", syncengine.StatePresent, d.Shell); err != nil {
		return nil, err
	}
	return s.Targets(), nil
}
