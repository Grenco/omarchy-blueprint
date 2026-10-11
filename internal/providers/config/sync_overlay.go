package config

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
)

// OverlaySyncTarget modifies only disposable staging, never the active profile.
func (Provider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, view syncprofile.View, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !view.Profile.Manifest.Capture.Config {
		view.Profile.Config = profile.Configs{}
	}
	if err := profile.ValidateConfigPath(target); err != nil {
		return err
	}
	src := profile.CloneData(view.Profile).Config
	d := &b.Profile.Config
	d.Files = syncprofile.Replace(d.Files, src.Files, target, func(x profile.ConfigFile) string { return x.Path })
	d.Deletes = syncprofile.Replace(d.Deletes, src.Deletes, target, func(x profile.ConfigDelete) string { return x.Path })
	for _, tree := range []string{"files", "baseline"} {
		if err := b.RemoveArtifact("config/" + tree + "/" + target); err != nil {
			return err
		}
	}
	for _, x := range src.Files {
		if x.Path == target {
			if err := b.ReplaceArtifact(ctx, view.Root, "config/files/"+target); err != nil {
				return err
			}
			if x.BaselineHash != "" {
				if err := b.ReplaceArtifact(ctx, view.Root, "config/baseline/"+target); err != nil {
					return err
				}
			}
		}
	}
	b.Profile.Manifest.Capture.Config = b.Profile.Manifest.Capture.Config || view.Profile.Manifest.Capture.Config
	return nil
}
