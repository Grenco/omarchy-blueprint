package services

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
)

// OverlaySyncTarget modifies only disposable staging, never the active profile.
func (*Provider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, view syncprofile.View, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !view.Profile.Manifest.Capture.Services {
		view.Profile.Services = profile.Services{}
	}
	src := profile.CloneData(view.Profile).Services
	for _, u := range b.Profile.Services.Units {
		if u.Name == target {
			for _, rel := range syncUnitArtifacts(u) {
				if err := b.RemoveArtifact("services/" + rel); err != nil {
					return err
				}
			}
		}
	}
	b.Profile.Services.Units = syncprofile.Replace(b.Profile.Services.Units, src.Units, target, func(x profile.ServiceUnit) string { return x.Name })
	for _, u := range src.Units {
		if u.Name == target && u.Presence == profile.ServicePresent {
			for _, rel := range syncUnitArtifacts(u) {
				if err := b.ReplaceArtifact(ctx, view.Root, "services/"+rel); err != nil {
					return err
				}
			}
		}
	}
	b.Profile.Manifest.Capture.Services = b.Profile.Manifest.Capture.Services || view.Profile.Manifest.Capture.Services
	return nil
}

func syncUnitArtifacts(u profile.ServiceUnit) []string {
	out := []string{}
	if u.Presence == profile.ServicePresent && u.Definition != "" {
		out = append(out, u.Definition)
	}
	for _, a := range u.DropIns {
		if a.Presence == profile.ServicePresent {
			out = append(out, a.Path)
		}
	}
	for _, i := range u.Instances {
		for _, a := range i.DropIns {
			if a.Presence == profile.ServicePresent {
				out = append(out, a.Path)
			}
		}
	}
	return out
}
