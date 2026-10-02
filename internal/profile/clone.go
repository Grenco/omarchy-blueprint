package profile

import (
	"reflect"
	"slices"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

// ClonePackages retains live-only facts as well as persisted desired state.
func ClonePackages(p Packages) Packages {
	p.Official = slices.Clone(p.Official)
	p.AUR = slices.Clone(p.AUR)
	if p.Mise != nil {
		tools := make(MiseTools, len(p.Mise))
		for id, tool := range p.Mise {
			tools[id] = cloneMiseTool(tool)
		}
		p.Mise = tools
	}
	p.MiseInstalled = cloneBoolMap(p.MiseInstalled)
	p.SemanticInstalled = cloneBoolMap(p.SemanticInstalled)
	p.SemanticRemoved = cloneBoolMap(p.SemanticRemoved)
	p.Preinstalls.Items = cloneBoolMap(p.Preinstalls.Items)
	p.Absent = slices.Clone(p.Absent)
	for i := range p.Absent {
		p.Absent[i].Mise = cloneMiseTool(p.Absent[i].Mise)
	}
	p.MachineSpecific = slices.Clone(p.MachineSpecific)
	p.Excluded = slices.Clone(p.Excluded)
	p.Installed = slices.Clone(p.Installed)
	p.MissingSyncDatabases = slices.Clone(p.MissingSyncDatabases)
	p.UnclassifiedExplicit = slices.Clone(p.UnclassifiedExplicit)
	return p
}

func cloneBoolMap(source map[string]bool) map[string]bool {
	if source == nil {
		return nil
	}
	result := make(map[string]bool, len(source))
	for k, v := range source {
		result[k] = v
	}
	return result
}

func cloneMiseTool(source MiseTool) MiseTool {
	if source == nil {
		return nil
	}
	result := make(MiseTool, len(source))
	for k, v := range source {
		if v != nil {
			result[k] = cloneMiseValue(reflect.ValueOf(v)).Interface()
		} else {
			result[k] = nil
		}
	}
	return result
}

// TOML values have concrete map/slice types, including typed arrays. Only
// collection nodes need copying; immutable scalar/date values retain types.
// This is intentionally private to Mise values, not a generic profile copier.
func cloneMiseValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(cloneMiseValue(v.Elem()))
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneMiseValue(iter.Value()))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneMiseValue(v.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneMiseValue(v.Index(i)))
		}
		return out
	default:
		return v
	}
}

func cloneRules(r policy.Rules) policy.Rules {
	r.Capture = slices.Clone(r.Capture)
	r.Restore = slices.Clone(r.Restore)
	return r
}
func cloneMask(mask *ServiceMask) *ServiceMask {
	if mask == nil {
		return nil
	}
	value := *mask
	return &value
}

// CloneData isolates every mutable collection and preserves nil versus empty.
// Scalar-only structures (including Shell metadata) copy by value.
func CloneData(d Data) Data {
	d.Packages = ClonePackages(d.Packages)
	d.Themes.Items = slices.Clone(d.Themes.Items)
	d.Themes.Absent = slices.Clone(d.Themes.Absent)
	d.Plugins.Items = slices.Clone(d.Plugins.Items)
	d.Plugins.Absent = slices.Clone(d.Plugins.Absent)
	d.Resources.Items = slices.Clone(d.Resources.Items)
	for i := range d.Resources.Items {
		d.Resources.Items[i].Untracked = slices.Clone(d.Resources.Items[i].Untracked)
	}
	d.Resources.Links = slices.Clone(d.Resources.Links)
	d.Resources.IgnoredLinks = slices.Clone(d.Resources.IgnoredLinks)
	d.Machines.Items = slices.Clone(d.Machines.Items)
	for i := range d.Machines.Items {
		d.Machines.Items[i].ResourcePaths = slices.Clone(d.Machines.Items[i].ResourcePaths)
		d.Machines.Items[i].Policy = cloneRules(d.Machines.Items[i].Policy)
	}
	d.Config.Files = slices.Clone(d.Config.Files)
	d.Config.Deletes = slices.Clone(d.Config.Deletes)
	d.Config.Included = slices.Clone(d.Config.Included)
	d.Config.Excluded = slices.Clone(d.Config.Excluded)
	d.Hooks.Items = slices.Clone(d.Hooks.Items)
	d.Hooks.Absent = slices.Clone(d.Hooks.Absent)
	d.Services.Units = slices.Clone(d.Services.Units)
	for i := range d.Services.Units {
		unit := &d.Services.Units[i]
		unit.Mask = cloneMask(unit.Mask)
		unit.DropIns = slices.Clone(unit.DropIns)
		unit.Instances = slices.Clone(unit.Instances)
		for j := range unit.Instances {
			instance := &unit.Instances[j]
			instance.Mask = cloneMask(instance.Mask)
			instance.DropIns = slices.Clone(instance.DropIns)
		}
	}
	d.Policy = cloneRules(d.Policy)
	return d
}
