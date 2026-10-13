package plugins

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
	"strings"
)

// OverlaySyncTarget modifies only disposable staging, never the active profile.
func (Provider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, view syncprofile.View, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !view.Profile.Manifest.Capture.Plugins {
		view.Profile.Plugins = profile.Plugins{}
	}
	src := profile.CloneData(view.Profile).Plugins
	id, ok := strings.CutPrefix(target, "plugin:")
	if !ok || id == "" {
		return fmt.Errorf("sync: invalid plugin target")
	}
	b.Profile.Plugins.Items = syncprofile.Replace(b.Profile.Plugins.Items, src.Items, id, func(x profile.Plugin) string { return x.ID })
	b.Profile.Plugins.Absent = syncprofile.Replace(b.Profile.Plugins.Absent, src.Absent, id, func(x profile.Plugin) string { return x.ID })
	rel := "plugins/local/" + id
	if err := b.RemoveArtifact(rel); err != nil {
		return err
	}
	for _, x := range src.Items {
		if x.ID == id && x.Source == "local" {
			if err := b.ReplaceArtifact(ctx, view.Root, rel); err != nil {
				return err
			}
		}
	}
	b.Profile.Manifest.Capture.Plugins = b.Profile.Manifest.Capture.Plugins || view.Profile.Manifest.Capture.Plugins
	return nil
}
