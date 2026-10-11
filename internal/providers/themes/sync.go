package themes

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// SyncDesired projects provider-owned semantic intent, not repository paths.
func (Provider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	s := syncengine.NewTargetSet("themes")
	if !d.Manifest.Capture.Themes {
		return s.Targets(), nil
	}
	if d.Themes.Current != "" {
		if err := s.Add("active", syncengine.StatePresent, d.Themes.Current); err != nil {
			return nil, err
		}
	}
	for _, t := range d.Themes.Items {
		if t.Type == "builtin" {
			continue
		}
		t.Enabled = false
		if err := s.Add("theme:"+t.ID, syncengine.StatePresent, t); err != nil {
			return nil, err
		}
	}
	for _, t := range d.Themes.Absent {
		t.Enabled = false
		if err := s.Add("theme:"+t.ID, syncengine.StateAbsent, t); err != nil {
			return nil, err
		}
	}
	return s.Targets(), nil
}
