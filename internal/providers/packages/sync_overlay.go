package packages

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
	"strings"
)

// OverlaySyncTarget modifies only disposable staging, never the active profile.
func (Provider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, view syncprofile.View, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	src := profile.CloneData(view.Profile).Packages
	d := &b.Profile.Packages
	d.Official = syncprofile.Replace(d.Official, src.Official, target, func(x string) string { return "official:" + x })
	d.AUR = syncprofile.Replace(d.AUR, src.AUR, target, func(x string) string { return "aur:" + x })
	d.Absent = syncprofile.Replace(d.Absent, src.Absent, target, func(x profile.PackageAbsence) string { return x.Ref })
	if id, ok := strings.CutPrefix(target, "mise:"); ok {
		if d.Mise == nil {
			d.Mise = profile.MiseTools{}
		}
		delete(d.Mise, id)
		if value, found := src.Mise[id]; found {
			d.Mise[id] = value
		}
	}
	if target == "preinstalls" {
		d.Preinstalls.Managed = src.Preinstalls.Managed
		d.Preinstalls.RemovedAll = src.Preinstalls.RemovedAll
	}
	if id, ok := strings.CutPrefix(target, "preinstall:"); ok {
		if d.Preinstalls.Items == nil {
			d.Preinstalls.Items = map[string]bool{}
		}
		delete(d.Preinstalls.Items, id)
		if v, found := src.Preinstalls.Items[id]; found {
			d.Preinstalls.Items[id] = v
		}
	}
	b.Profile.Manifest.Capture.Packages = b.Profile.Manifest.Capture.Packages || view.Profile.Manifest.Capture.Packages
	return nil
}
