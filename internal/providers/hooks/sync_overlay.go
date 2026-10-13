package hooks

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
	if !view.Profile.Manifest.Capture.Hooks {
		view.Profile.Hooks = profile.Hooks{}
	}
	if err := ValidatePath(target); err != nil {
		return err
	}
	src := profile.CloneData(view.Profile).Hooks
	d := &b.Profile.Hooks
	d.Items = syncprofile.Replace(d.Items, src.Items, target, func(x profile.Hook) string { return x.Path })
	d.Absent = syncprofile.Replace(d.Absent, src.Absent, target, func(x profile.Hook) string { return x.Path })
	rel := "hooks/files/" + target
	if err := b.RemoveArtifact(rel); err != nil {
		return err
	}
	for _, x := range src.Items {
		if x.Path == target {
			if err := b.ReplaceArtifact(ctx, view.Root, rel); err != nil {
				return err
			}
		}
	}
	b.Profile.Manifest.Capture.Hooks = b.Profile.Manifest.Capture.Hooks || view.Profile.Manifest.Capture.Hooks
	return nil
}
