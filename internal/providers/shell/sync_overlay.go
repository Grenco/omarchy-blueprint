package shell

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
)

// OverlaySyncTarget modifies only disposable staging, never the active profile.
func (Provider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, view syncprofile.View, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !view.Profile.Manifest.Capture.Shell {
		view.Profile.Shell = profile.Shell{}
	}
	if target != "state" {
		return fmt.Errorf("sync: invalid shell target")
	}
	b.Profile.Shell = view.Profile.Shell
	for _, rel := range []string{"shell/shell.json", "shell/baseline.json"} {
		if err := b.RemoveArtifact(rel); err != nil {
			return err
		}
		if view.Profile.Shell.Hash != "" {
			if err := b.ReplaceArtifact(ctx, view.Root, rel); err != nil {
				return err
			}
		}
	}
	b.Profile.Manifest.Capture.Shell = b.Profile.Manifest.Capture.Shell || view.Profile.Manifest.Capture.Shell
	return nil
}
