package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Plan creates only additive or mode-repair operations. It never overwrites
// differing hook source and never removes target hooks.
func (p Provider) Plan(saved profile.Hooks, current State, schema int, from, to string) (model.RestorePlan, error) {
	if err := ValidateMetadata(saved.Items); err != nil {
		return model.RestorePlan{}, err
	}
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	actual := currentMap(current.Items)
	unmanaged := make(map[string]UnmanagedHook, len(current.Unmanaged))
	for _, item := range current.Unmanaged {
		unmanaged[item.Path] = item
	}
	desired := append([]profile.Hook(nil), saved.Items...)
	sort.Slice(desired, func(i, j int) bool { return desired[i].Path < desired[j].Path })
	for _, item := range desired {
		mode, err := ParseMode(item.Mode)
		if err != nil {
			return model.RestorePlan{}, err
		}
		resource := "hook:" + item.Path
		current, exists := actual[item.Path]
		delete(actual, item.Path)
		if reason, blocked := blockedByUnmanaged(item.Path, unmanaged); blocked {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "hooks", Resource: resource, Reason: reason})
			continue
		}
		if exists && current.Hash != item.Hash {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "hooks", Resource: resource, Reason: "existing hook content differs; overwrite disabled"})
			continue
		}
		if exists && current.Mode == item.Mode {
			continue
		}
		source := filepath.Join(p.ProfileDir, "hooks", "files", filepath.FromSlash(item.Path))
		if _, err := os.Lstat(source); err != nil {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "hooks", Resource: resource, Reason: "captured hook file is missing from the profile"})
			continue
		}
		write := model.FileWrite{Source: source, Destination: filepath.Join(p.UserDir, filepath.FromSlash(item.Path)), SourceHash: item.Hash, Mode: &mode}
		if exists {
			expectedMode, err := ParseMode(current.Mode)
			if err != nil {
				return model.RestorePlan{}, fmt.Errorf("current hook %q: %w", item.Path, err)
			}
			write.ExpectedHash = item.Hash
			write.ExpectedMode = &expectedMode
			write.Backup = true
		} else {
			write.ExpectedMissing = true
		}
		plan.Operations = append(plan.Operations, model.Operation{ID: operationID(item.Path), Provider: "hooks", Action: "write", Resource: resource, Items: []string{item.Path}, File: &write, Risk: model.RiskHigh, Reversible: exists})
	}
	paths := make([]string, 0, len(actual))
	for path := range actual {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "hooks", Resource: "hook:" + path, Reason: "extra target hook preserved; removal disabled"})
	}
	return plan, nil
}

func blockedByUnmanaged(path string, unmanaged map[string]UnmanagedHook) (string, bool) {
	if _, ok := unmanaged[path]; ok {
		return "existing hook is an unmanaged symlink; overwrite disabled", true
	}
	parent, _, hasChild := strings.Cut(path, "/")
	if hasChild {
		if _, ok := unmanaged[parent]; ok {
			return "hook directory " + parent + " is an unmanaged symlink; restore into it disabled", true
		}
	}
	return "", false
}

func operationID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return "hooks.write." + hex.EncodeToString(sum[:8])
}
