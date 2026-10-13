package app

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func (p restoreProviderAdapter) ValidateSyncArtifacts(ctx context.Context, root string, d profile.Data) error {
	if v, ok := p.stateProvider.(interface {
		ValidateSyncArtifacts(context.Context, string, profile.Data) error
	}); ok {
		return v.ValidateSyncArtifacts(ctx, root, profile.CloneData(d))
	}
	// Packages and Defaults store semantic metadata, not snapshot artifacts.
	return ctx.Err()
}
