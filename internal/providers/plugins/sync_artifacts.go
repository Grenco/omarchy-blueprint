package plugins

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
	if !d.Manifest.Capture.Plugins {
		return nil
	}
	for _, x := range d.Plugins.Items {
		if x.Source == "local" {
			got, err := hashTree(filepath.Join(root, "plugins", "local", x.ID))
			if err != nil || got != x.Hash {
				return fmt.Errorf("sync: invalid plugin snapshot")
			}
		}
	}
	return ctx.Err()
}
