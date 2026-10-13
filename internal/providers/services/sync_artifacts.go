package services

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// ValidateSyncArtifacts checks only isolated historical bytes, never live state.
func (*Provider) ValidateSyncArtifacts(ctx context.Context, root string, d profile.Data) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.Manifest.Capture.Services {
		return nil
	}
	p := &Provider{ProfileDir: root}
	if err := p.Check(ctx, d); err != nil {
		return err
	}
	for _, x := range d.Services.Units {
		for _, i := range x.Instances {
			for _, a := range i.DropIns {
				if a.Presence == profile.ServicePresent {
					if err := p.checkServiceArtifact(a.Path, a.Hash); err != nil {
						return err
					}
				}
			}
		}
	}
	return ctx.Err()
}
