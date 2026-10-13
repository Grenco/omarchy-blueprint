package resources

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"sort"
)

// SyncDesired projects provider-owned semantic intent, not repository paths.
func (Provider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	s := syncengine.NewTargetSet("resources")
	if !d.Manifest.Capture.Resources {
		return s.Targets(), nil
	}
	data := profile.CloneData(d)
	for _, r := range data.Resources.Items {
		r.Dirty = false
		// Portable paths and machine mappings are interpretation inputs,
		// fingerprinted independently by the workflow control plane.
		r.Path = ""
		sort.Slice(r.Untracked, func(i, j int) bool { return r.Untracked[i].Path < r.Untracked[j].Path })
		links := []profile.ResourceLink{}
		for _, l := range data.Resources.Links {
			if l.Origin == "resource" && l.SourceResource == r.ID || l.Origin == "inbound" && l.TargetResource == r.ID {
				links = append(links, l)
			}
		}
		sort.Slice(links, func(i, j int) bool {
			a, b := links[i], links[j]
			if a.Origin != b.Origin {
				return a.Origin < b.Origin
			}
			if a.SourceResource != b.SourceResource {
				return a.SourceResource < b.SourceResource
			}
			if a.Source != b.Source {
				return a.Source < b.Source
			}
			if a.TargetResource != b.TargetResource {
				return a.TargetResource < b.TargetResource
			}
			return a.Target < b.Target
		})
		if err := s.Add("resource:"+r.ID, syncengine.StatePresent, struct {
			Resource profile.Resource
			Links    []profile.ResourceLink
		}{r, links}); err != nil {
			return nil, err
		}
	}
	return s.Targets(), nil
}
