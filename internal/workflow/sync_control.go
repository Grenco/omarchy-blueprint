package workflow

import (
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"reflect"
	"sort"
)

// Config scope and resource-link exclusions influence what local-hint discovery
// can see. A changed selected interpretation cannot be certified by that scan.
// Policy-only changes do not affect acquisition of the frozen machine facts.
func syncObservationScopeChanged(local, chosen profile.Data) bool {
	sameSet := func(a, b []string) bool {
		set := func(xs []string) map[string]bool {
			out := map[string]bool{}
			for _, x := range xs {
				out[x] = true
			}
			return out
		}
		return reflect.DeepEqual(set(a), set(b))
	}
	return !sameSet(local.Config.Included, chosen.Config.Included) ||
		!sameSet(local.Config.Excluded, chosen.Config.Excluded) ||
		!sameSet(local.Resources.IgnoredLinks, chosen.Resources.IgnoredLinks)
}

// A read cycle is bound to the current machine selection. Do not certify a
// different resource interpretation using observations of the old roots.
func syncResourceInterpretationChanged(local, chosen profile.Data, selected string) bool {
	paths := func(d profile.Data) map[string]string {
		out := map[string]string{}
		for _, r := range d.Resources.Items {
			out[r.ID] = r.Path
		}
		for _, m := range d.Machines.Items {
			if m.Name == selected {
				for _, mapping := range m.ResourcePaths {
					out[mapping.Resource] = mapping.Path
				}
			}
		}
		return out
	}
	return !reflect.DeepEqual(paths(local), paths(chosen))
}

// Control state is separate from runtime targets and includes all definitions,
// not just today's selected overlay. Two-sided disagreement is never merged by
// target resolution, and no remotely authored policy grants mutation authority.
type SyncControlState struct{ Fingerprint string }

func syncControl(data profile.Data, selected string) (SyncControlState, error) {
	d := profile.CloneData(data)
	rules := func(r *policy.Rules) {
		less := func(xs []policy.Rule) {
			sort.Slice(xs, func(i, j int) bool {
				if xs[i].Category != xs[j].Category {
					return xs[i].Category < xs[j].Category
				}
				return xs[i].Target < xs[j].Target
			})
		}
		less(r.Capture)
		less(r.Restore)
	}
	rules(&d.Policy)
	for i := range d.Machines.Items {
		m := &d.Machines.Items[i]
		rules(&m.Policy)
		sort.Slice(m.ResourcePaths, func(a, b int) bool { return m.ResourcePaths[a].Resource < m.ResourcePaths[b].Resource })
	}
	sort.Slice(d.Machines.Items, func(i, j int) bool { return d.Machines.Items[i].Name < d.Machines.Items[j].Name })
	// Paths are interpretation inputs, not an ordinary resource target value.
	paths := map[string]string{}
	for _, r := range d.Resources.Items {
		paths[r.ID] = r.Path
	}
	normalize := func(xs []string) []string {
		set := map[string]bool{}
		for _, x := range xs {
			set[x] = true
		}
		out := make([]string, 0, len(set))
		for x := range set {
			out = append(out, x)
		}
		sort.Strings(out)
		return out
	}
	hash, err := syncengine.Fingerprint(struct {
		Selected                                                      string
		Schema                                                        int
		Omarchy                                                       profile.OmarchyMeta
		Capture                                                       profile.CaptureMeta
		Machines                                                      []profile.Machine
		Policy                                                        policy.Rules
		ResourcePaths                                                 map[string]string
		ConfigIncluded, ConfigExcluded, IgnoredLinks, PackageExcluded []string
	}{selected, d.Manifest.Schema, d.Manifest.Omarchy, d.Manifest.Capture, d.Machines.Items, d.Policy, paths, normalize(d.Config.Included), normalize(d.Config.Excluded), normalize(d.Resources.IgnoredLinks), normalize(d.Packages.Excluded)})
	return SyncControlState{Fingerprint: hash}, err
}

func chooseSyncControl(base, local, remote SyncControlState) (bool, bool) {
	control := classifySyncControl(base, local, remote)
	return control.SelectedSource == syncengine.ControlSourceLocal, control.History == syncengine.HistoryConflict
}

func classifySyncControl(base, local, remote SyncControlState) syncengine.ControlChange {
	// Whole-plane conservative conflict detection is intentional v1:
	// independent two-sided policy edits still require external review.
	control := syncengine.ControlChange{Base: base.Fingerprint, Local: local.Fingerprint, Remote: remote.Fingerprint, SelectedSource: syncengine.ControlSourceRemote}
	switch {
	case local.Fingerprint == base.Fingerprint && remote.Fingerprint == base.Fingerprint:
		control.History = syncengine.HistoryUnchanged
	case local.Fingerprint == base.Fingerprint:
		control.History = syncengine.HistoryRemote
	case remote.Fingerprint == base.Fingerprint:
		control.History = syncengine.HistoryLocal
		control.SelectedSource = syncengine.ControlSourceLocal
	case local.Fingerprint == remote.Fingerprint:
		control.History = syncengine.HistoryConverged
	default:
		control.History = syncengine.HistoryConflict
		control.SelectedSource = syncengine.ControlSourceNone
	}
	return control
}

func overlaySyncControl(dst *profile.Data, src profile.Data) {
	src = profile.CloneData(src)
	dst.Manifest.Schema = src.Manifest.Schema
	dst.Manifest.Omarchy = src.Manifest.Omarchy
	dst.Manifest.Capture = src.Manifest.Capture
	dst.Machines = src.Machines
	dst.Policy = src.Policy
	dst.Config.Included = src.Config.Included
	dst.Config.Excluded = src.Config.Excluded
	dst.Resources.IgnoredLinks = src.Resources.IgnoredLinks
	dst.Packages.Excluded = src.Packages.Excluded
	for i := range dst.Resources.Items {
		for _, r := range src.Resources.Items {
			if dst.Resources.Items[i].ID == r.ID {
				dst.Resources.Items[i].Path = r.Path
			}
		}
	}
}
