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
		t = normalizeSyncTheme(t)
		if err := s.Add("theme:"+t.ID, syncengine.StatePresent, t); err != nil {
			return nil, err
		}
	}
	for _, t := range d.Themes.Absent {
		t = normalizeSyncTheme(t)
		if err := s.Add("theme:"+t.ID, syncengine.StateAbsent, t); err != nil {
			return nil, err
		}
	}
	return s.Targets(), nil
}

func normalizeSyncTheme(t profile.Theme) profile.Theme {
	// Match provider equivalence rather than incidental capture metadata.
	out := profile.Theme{ID: t.ID, Type: t.Type}
	switch t.Type {
	case "git":
		out.URL, out.Revision = t.URL, t.Revision
	case "local", "overlay":
		out.Hash = t.Hash
	}
	return out
}
