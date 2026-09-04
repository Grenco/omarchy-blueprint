package shell

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

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
		return nil, nil
	}
	analysis, err := p.Analyze(saved, current, MergeOptions{})
	if err != nil {
		return nil, err
	}
	changes := make([]model.Change, 0, len(analysis.Applied)+len(analysis.Conflicts))
	for _, applied := range analysis.Applied {
		name := pathName(applied.Path)
		if isScalar(applied.Before) && isScalar(applied.After) {
			changes = append(changes, change(model.ChangeModify, name, fmt.Sprintf("~ %s: %s -> %s", name, canonicalValue(applied.Before), canonicalValue(applied.After))))
		} else {
			changes = append(changes, change(model.ChangeModify, name, fmt.Sprintf("~ %s differs", name)))
		}
	}
	for _, conflict := range analysis.Conflicts {
		name := pathName(conflict.Path)
		changes = append(changes, change(model.ChangeWarn, name, fmt.Sprintf("! %s changed independently; current target value preserved", name)))
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes, nil
}

func (p Provider) Verify(saved profile.Shell, current State) (model.VerificationResult, error) {
	if saved.Hash == "" {
		return model.VerificationResult{OK: true}, nil
	}
	analysis, err := p.Analyze(saved, current, MergeOptions{})
	if err != nil {
		return model.VerificationResult{}, err
	}
	missing := make([]string, 0, len(analysis.Applied)+len(analysis.Conflicts))
	seen := map[string]bool{}
	for _, applied := range analysis.Applied {
		name := "shell:" + pathName(applied.Path)
		if !seen[name] {
			seen[name] = true
			missing = append(missing, name)
		}
	}
	for _, conflict := range analysis.Conflicts {
		name := "shell:" + pathName(conflict.Path)
		if !seen[name] {
			seen[name] = true
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}, nil
}

func isScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, json.Number, float64:
		return true
	default:
		return false
	}
}

func canonicalValue(value any) string {
	if value == nil {
		return "null"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "<error>"
	}
	return string(encoded)
}

func change(kind model.ChangeType, name, summary string) model.Change {
	return model.Change{Type: kind, Provider: "shell", Kind: "shell", Name: name, Summary: summary}
}
