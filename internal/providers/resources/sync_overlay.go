package resources

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
	if !view.Profile.Manifest.Capture.Resources {
		view.Profile.Resources = profile.Resources{}
	}
	src := profile.CloneData(view.Profile).Resources
	id, ok := strings.CutPrefix(target, "resource:")
	if !ok || id == "" {
		return fmt.Errorf("sync: invalid resource target")
	}
	b.Profile.Resources.Items = syncprofile.Replace(b.Profile.Resources.Items, src.Items, id, func(x profile.Resource) string { return x.ID })
	owner := func(x profile.ResourceLink) string {
		if x.Origin == "resource" {
			return x.SourceResource
		}
		return x.TargetResource
	}
	b.Profile.Resources.Links = syncprofile.Replace(b.Profile.Resources.Links, src.Links, id, owner)
	for _, rel := range []string{"resources/files/" + id, "resources/git-state/" + id} {
		if err := b.RemoveArtifact(rel); err != nil {
			return err
		}
	}
	for _, x := range src.Items {
		if x.ID == id {
			if x.Strategy == "copy" {
				if err := b.ReplaceArtifact(ctx, view.Root, "resources/files/"+id); err != nil {
					return err
				}
			}
			if x.IndexPatchHash != "" || x.WorktreePatchHash != "" || len(x.Untracked) > 0 {
				if err := b.ReplaceArtifact(ctx, view.Root, "resources/git-state/"+id); err != nil {
					return err
				}
			}
		}
	}
	b.Profile.Manifest.Capture.Resources = b.Profile.Manifest.Capture.Resources || view.Profile.Manifest.Capture.Resources
	return nil
}
