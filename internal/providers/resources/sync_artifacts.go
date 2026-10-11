package resources

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"path/filepath"
)

// ValidateSyncArtifacts checks only isolated historical bytes, never live state.
func (Provider) ValidateSyncArtifacts(ctx context.Context, root string, d profile.Data) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.Manifest.Capture.Resources {
		return nil
	}
	for _, x := range d.Resources.Items {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch x.Strategy {
		case "copy":
			if err := ValidateSnapshotTree(resourceSnapshotPath(filepath.Join(root, "resources"), x), x.Hash); err != nil {
				return err
			}
		case "git+diff":
			state := filepath.Join(root, "resources", "git-state", x.ID)
			if err := validateGitStateArtifact(filepath.Join(state, "index.patch"), x.IndexPatchHash); err != nil {
				return err
			}
			if err := validateGitStateArtifact(filepath.Join(state, "worktree.patch"), x.WorktreePatchHash); err != nil {
				return err
			}
			for _, file := range x.Untracked {
				if err := validateGitStateArtifact(filepath.Join(state, "untracked", filepath.FromSlash(file.Path)), file.Hash); err != nil {
					return err
				}
			}
		}
	}
	return ctx.Err()
}
