package themes

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Grenco/omarchy-blueprint/internal/portabletree"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// ValidateSyncArtifacts checks only isolated historical bytes, never live state.
func (Provider) ValidateSyncArtifacts(ctx context.Context, root string, d profile.Data) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.Manifest.Capture.Themes {
		return nil
	}
	for _, x := range d.Themes.Items {
		if x.Type == "local" || x.Type == "overlay" {
			dir := filepath.Join(root, "themes", "local", x.ID)
			if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
				return &syncengine.ArtifactError{Target: "theme:" + x.ID, Missing: true}
			}
			// The same portable-snapshot contract Capture recorded it with.
			got, err := portabletree.Fingerprint(dir)
			if err != nil || got != x.Hash {
				return &syncengine.ArtifactError{Target: "theme:" + x.ID}
			}
		}
	}
	return ctx.Err()
}
