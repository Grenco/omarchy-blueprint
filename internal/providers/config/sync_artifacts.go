package config

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// ValidateSyncArtifacts checks only isolated historical bytes, never live state.
func (Provider) ValidateSyncArtifacts(ctx context.Context, root string, d profile.Data) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.Manifest.Capture.Config {
		return nil
	}
	p := Provider{ProfileDir: root}
	for _, x := range d.Config.Files {
		if err := p.checkOverlaySnapshot("files", x.Path, x.Hash); err != nil {
			return err
		}
		if x.BaselineHash != "" {
			if err := p.checkOverlaySnapshot("baseline", x.Path, x.BaselineHash); err != nil {
				return err
			}
		}
	}
	for _, x := range d.Config.Deletes {
		if err := p.checkOverlaySnapshot("baseline", x.Path, x.BaselineHash); err != nil {
			return err
		}
	}
	return ctx.Err()
}
