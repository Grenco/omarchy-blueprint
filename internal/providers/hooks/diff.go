package hooks

import (
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func DiffCaptures(previous, next profile.Hooks) []model.Change {
	return compare(savedMap(previous.Items), currentMapFromSaved(next.Items))
}

func Diff(saved profile.Hooks, current State) []model.Change {
	changes := compare(savedMap(saved.Items), currentMap(current.Items))
	return append(changes, UnmanagedWarnings(current.Unmanaged)...)
}

func UnmanagedWarnings(items []UnmanagedHook) []model.Change {
	warnings := make([]model.Change, 0, len(items))
	for _, item := range items {
		summary := "! hook " + item.Path + " is a symlink and was left unmanaged"
		if item.Broken {
			summary = "! hook " + item.Path + " is a broken symlink and was left unmanaged"
		} else if item.Target != "" {
			summary += " -> " + item.Target
		}
		warnings = append(warnings, hookChange(model.ChangeWarn, item.Path, summary))
	}
	sort.Slice(warnings, func(i, j int) bool { return warnings[i].Name < warnings[j].Name })
	return warnings
}

func compare(saved map[string]profile.Hook, current map[string]DetectedHook) []model.Change {
	changes := make([]model.Change, 0)
	for path, item := range current {
		desired, ok := saved[path]
		if !ok {
			changes = append(changes, hookChange(model.ChangeAdd, path, "+ hook "+path))
			continue
		}
		delete(saved, path)
		if desired.Hash == item.Hash && desired.Mode == item.Mode {
			continue
		}
		switch {
		case desired.Hash != item.Hash && desired.Mode != item.Mode:
			changes = append(changes, hookChange(model.ChangeModify, path, "~ hook "+path+" content and mode changed ("+desired.Mode+" → "+item.Mode+")"))
		case desired.Hash != item.Hash:
			changes = append(changes, hookChange(model.ChangeModify, path, "~ hook "+path+" changed"))
		default:
			changes = append(changes, hookChange(model.ChangeModify, path, "~ hook "+path+" mode "+desired.Mode+" → "+item.Mode))
		}
	}
	for path := range saved {
		changes = append(changes, hookChange(model.ChangeRemove, path, "- hook "+path))
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes
}

func Verify(saved profile.Hooks, current State) model.VerificationResult {
	actual := currentMap(current.Items)
	missing := make([]string, 0)
	for _, item := range saved.Items {
		current, ok := actual[item.Path]
		if !ok || current.Hash != item.Hash || current.Mode != item.Mode {
			missing = append(missing, "hook:"+item.Path)
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}

func savedMap(items []profile.Hook) map[string]profile.Hook {
	result := make(map[string]profile.Hook, len(items))
	for _, item := range items {
		result[item.Path] = item
	}
	return result
}

func currentMap(items []DetectedHook) map[string]DetectedHook {
	result := make(map[string]DetectedHook, len(items))
	for _, item := range items {
		result[item.Path] = item
	}
	return result
}

func currentMapFromSaved(items []profile.Hook) map[string]DetectedHook {
	result := make(map[string]DetectedHook, len(items))
	for _, item := range items {
		result[item.Path] = DetectedHook{Path: item.Path, Hash: item.Hash, Mode: item.Mode}
	}
	return result
}

func hookChange(kind model.ChangeType, path, summary string) model.Change {
	return model.Change{Type: kind, Provider: "hooks", Kind: "hook", Name: path, Summary: summary}
}
