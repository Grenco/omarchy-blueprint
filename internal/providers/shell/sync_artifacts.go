package shell

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// ValidateSyncArtifacts checks only isolated historical bytes, never live state.
func (Provider) ValidateSyncArtifacts(ctx context.Context, root string, d profile.Data) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.Manifest.Capture.Shell {
		return nil
	}
	if err := (Provider{ProfileDir: root}).Check(d.Shell, d.Plugins); err != nil {
		return err
	}
	return ctx.Err()
}
