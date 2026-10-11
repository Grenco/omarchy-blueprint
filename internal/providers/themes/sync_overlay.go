package themes

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
	if !view.Profile.Manifest.Capture.Themes {
		view.Profile.Themes = profile.Themes{}
	}
	src := profile.CloneData(view.Profile).Themes
	if target == "active" {
		b.Profile.Themes.Current = src.Current
		b.Profile.Themes.Source = src.Source
	} else {
		id, ok := strings.CutPrefix(target, "theme:")
		if !ok || id == "" {
			return fmt.Errorf("sync: invalid theme target")
		}
		b.Profile.Themes.Items = syncprofile.Replace(b.Profile.Themes.Items, src.Items, id, func(x profile.Theme) string { return x.ID })
		b.Profile.Themes.Absent = syncprofile.Replace(b.Profile.Themes.Absent, src.Absent, id, func(x profile.Theme) string { return x.ID })
		rel := "themes/local/" + id
		if err := b.RemoveArtifact(rel); err != nil {
			return err
		}
		for _, x := range src.Items {
			if x.ID == id && (x.Type == "local" || x.Type == "overlay") {
				if err := b.ReplaceArtifact(ctx, view.Root, rel); err != nil {
					return err
				}
			}
		}
	}
	b.Profile.Manifest.Capture.Themes = b.Profile.Manifest.Capture.Themes || view.Profile.Manifest.Capture.Themes
	return nil
}
