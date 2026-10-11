package themes

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"path/filepath"
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
			got, err := hashTree(filepath.Join(root, "themes", "local", x.ID))
			if err != nil || got != x.Hash {
				return fmt.Errorf("sync: invalid theme snapshot")
			}
		}
	}
	return ctx.Err()
}
