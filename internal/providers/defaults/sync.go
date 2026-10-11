package defaults

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// SyncDesired projects provider-owned semantic intent, not repository paths.
func (Provider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	s := syncengine.NewTargetSet("defaults")
	if !d.Manifest.Capture.Defaults {
		return s.Targets(), nil
	}
	for key, value := range map[string]string{"terminal": d.Defaults.Terminal, "browser": d.Defaults.Browser, "editor": d.Defaults.Editor, "agent": d.Defaults.Agent} {
		if value != "" {
			if err := s.Add(key, syncengine.StatePresent, value); err != nil {
				return nil, err
			}
		}
	}
	return s.Targets(), nil
}
