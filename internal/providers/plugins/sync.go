package plugins

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// SyncDesired projects provider-owned semantic intent, not repository paths.
func (Provider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	s := syncengine.NewTargetSet("plugins")
	if !d.Manifest.Capture.Plugins {
		return s.Targets(), nil
	}
	add := func(t profile.Plugin, kind syncengine.StateKind) error {
		if d.Manifest.Capture.Shell {
			t.Enabled = false
		}
		if t.Source == "" {
			t.Source = "builtin"
		}
		return s.Add("plugin:"+t.ID, kind, t)
	}
	for _, t := range d.Plugins.Items {
		if err := add(t, syncengine.StatePresent); err != nil {
			return nil, err
		}
	}
	for _, t := range d.Plugins.Absent {
		if err := add(t, syncengine.StateAbsent); err != nil {
			return nil, err
		}
	}
	return s.Targets(), nil
}
