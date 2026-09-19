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

// PlanOptions controls Exact-only behavior; the zero value (Additive) never
// deletes anything, matching every existing caller that predates it.
type PlanOptions struct{ Exact bool }

// Plan creates only additive or mode-repair operations under Additive
// convergence. It never overwrites differing hook source and never removes
// target hooks it did not previously manage. Under Exact, a
// provenance-matched Hooks.Absent tombstone may additionally produce a
// delete operation -- see the loop below.
func (p Provider) Plan(saved profile.Hooks, current State, schema int, from, to string, options ...PlanOptions) (model.RestorePlan, error) {
	var opts PlanOptions
	if len(options) > 0 {
		opts = options[0]
	}
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
		reserved, err := p.reservesInboundLink(filepath.Join(p.UserDir, filepath.FromSlash(item.Path)))
		if err != nil {
			return model.RestorePlan{}, err
		}
		if reserved {
			continue
		}
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
	// A hook tombstoned in saved.Absent is not "extra" -- it has its own
	// recorded desired state (desired-absent), just not Present state.
	// Excluding it from actual here, regardless of Convergence, keeps it out
	// of the generic "extra target hook preserved" reporting below: under
	// Additive it is silently left alone entirely (matching Packages/
	// Themes/Plugins' established invariant that Additive never reads
	// Absent at all); under Exact it gets its own, more specific
	// skip/removal handling.
	desiredAbsent := append([]profile.Hook(nil), saved.Absent...)
	sort.Slice(desiredAbsent, func(i, j int) bool { return desiredAbsent[i].Path < desiredAbsent[j].Path })
	for _, absent := range desiredAbsent {
		currentHook, exists := actual[absent.Path]
		delete(actual, absent.Path)
		if !opts.Exact {
			continue
		}
		resource := "hook:" + absent.Path
		// blockedByUnmanaged must be checked before the exists/hash
		// comparison below, matching the Items write-path's own order: an
		// unmanaged symlink at this path never appears in actual (Detect
		// reports it separately), so exists would otherwise read as "already
		// missing" and skip the unmanaged check entirely.
		if reason, blocked := blockedByUnmanaged(absent.Path, unmanaged); blocked {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "hooks", Resource: resource, Reason: reason})
			continue
		}
		if !exists {
			continue
		}
		reserved, err := p.reservesInboundLink(filepath.Join(p.UserDir, filepath.FromSlash(absent.Path)))
		if err != nil {
			return model.RestorePlan{}, err
		}
		if reserved {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "hooks", Resource: resource, Reason: "path is reserved for a tracked Resources link; deletion disabled"})
			continue
		}
		if currentHook.Hash != absent.Hash || currentHook.Mode != absent.Mode {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "hooks", Resource: resource, Reason: "existing hook content differs from the removed profile entry; deletion skipped"})
			continue
		}
		mode, err := ParseMode(absent.Mode)
		if err != nil {
			return model.RestorePlan{}, fmt.Errorf("tombstoned hook %q: %w", absent.Path, err)
		}
		plan.Operations = append(plan.Operations, model.Operation{
			ID: deleteOperationID(absent.Path), Provider: "hooks", Action: "delete", Resource: resource, Items: []string{absent.Path},
			Delete: &model.FileDelete{
				Destination:          filepath.Join(p.UserDir, filepath.FromSlash(absent.Path)),
				ExpectedExisting:     &model.FilesystemPrecondition{Type: "file", Hash: absent.Hash, Mode: mode},
				Backup:               true,
				RejectSymlinkParents: true,
			},
			Risk: model.RiskHigh, Reversible: true,
		})
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

func deleteOperationID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return "hooks.delete." + hex.EncodeToString(sum[:8])
}
