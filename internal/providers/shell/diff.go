package shell

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// scalarPaths are known Shell fields compared by canonical scalar value.
var scalarPaths = [][]string{
	{"bar", "id"},
	{"bar", "position"},
	{"bar", "transparent"},
	{"bar", "centerAnchor"},
	{"idle", "screensaver"},
	{"idle", "lock"},
}

// structuredPaths are known Shell fields compared as canonical subtrees.
var structuredPaths = [][]string{
	{"bar", "layout", "left"},
	{"bar", "layout", "center"},
	{"bar", "layout", "right"},
	{"plugins"},
	{"disabledPlugins"},
}

// pathName renders a field path as a stable change name.
func pathName(path []string) string {
	name := ""
	for _, part := range path {
		if name == "" {
			name = part
			continue
		}
		name += "." + part
	}
	return name
}

func (p Provider) Diff(saved profile.Shell, current State) ([]model.Change, error) {
	if saved.Hash == "" {
		switch current.Status {
		case StatusCustomized:
			return []model.Change{change(model.ChangeAdd, "shell", "+ shell customization present")}, nil
		case StatusUnsupported:
			return []model.Change{change(model.ChangeWarn, "shell", "! shell JSON version "+fmt.Sprint(current.Version)+" is not supported by this Blueprint version")}, nil
		}
		return nil, nil
	}
	if current.Status == StatusUnsupported {
		return []model.Change{change(model.ChangeWarn, "shell", fmt.Sprintf("! shell JSON version %d changed; migration required", current.Version))}, nil
	}
	if current.Status == StatusDefault {
		return []model.Change{change(model.ChangeRemove, "shell", "- shell customization removed (machine uses the Omarchy default)")}, nil
	}
	if current.Status == StatusCustomized && current.Hash == saved.Hash {
		return nil, nil
	}
	// Both documents exist and differ: compare known fields semantically.
	changes := p.compareKnownFields(saved, current)
	if len(changes) == 0 {
		changes = append(changes, change(model.ChangeModify, "shell", "~ shell configuration differs"))
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes, nil
}

func (p Provider) compareKnownFields(saved profile.Shell, current State) []model.Change {
	var changes []model.Change
	desired := p.savedValue(saved)
	if desired == nil {
		return nil
	}
	for _, path := range scalarPaths {
		before, _ := valueAt(desired, path...)
		after, _ := valueAt(current.Current.Value, path...)
		if canonicalValue(before) != canonicalValue(after) {
			name := pathName(path)
			changes = append(changes, change(model.ChangeModify, name,
				fmt.Sprintf("~ %s: %s → %s", name, canonicalValue(before), canonicalValue(after))))
		}
	}
	for _, path := range structuredPaths {
		before, _ := valueAt(desired, path...)
		after, _ := valueAt(current.Current.Value, path...)
		if canonicalValue(before) != canonicalValue(after) {
			changes = append(changes, change(model.ChangeModify, pathName(path),
				fmt.Sprintf("~ %s differs", pathName(path))))
		}
	}
	return changes
}

// savedValue loads the desired Shell document from the profile snapshot.
func (p Provider) savedValue(saved profile.Shell) map[string]any {
	if saved.Hash == "" || p.ProfileDir == "" {
		return nil
	}
	doc, err := ReadDocument(filepath.Join(p.ProfileDir, "shell", "shell.json"))
	if err != nil {
		return nil
	}
	return doc.Value
}

// jsonMarshal wraps encoding/json.Marshal.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func change(kind model.ChangeType, name, summary string) model.Change {
	return model.Change{Type: kind, Provider: "shell", Kind: "shell", Name: name, Summary: summary}
}

// Verify is asymmetric: extra machine Shell customization never fails
// verification; a desired customization must exist with the recorded version
// and canonical hash.
func Verify(saved profile.Shell, current State) model.VerificationResult {
	if saved.Hash == "" {
		return model.VerificationResult{OK: true}
	}
	if current.Status == StatusCustomized &&
		current.Version == saved.Version &&
		current.Hash == saved.Hash {
		return model.VerificationResult{OK: true}
	}
	return model.VerificationResult{
		OK:      false,
		Missing: []string{"shell:config"},
	}
}

// valueAt walks a decoded JSON object by path.
func valueAt(root map[string]any, path ...string) (any, bool) {
	var current any = root
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[key]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// canonicalValue renders any decoded JSON value deterministically.
func canonicalValue(v any) string {
	if v == nil {
		return "<absent>"
	}
	b, err := jsonMarshal(v)
	if err != nil {
		return "<error>"
	}
	return string(b)
}
