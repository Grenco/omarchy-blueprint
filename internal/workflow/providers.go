package workflow

import (
	"context"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// RestoreProvider extends a provider with restore planning and verification.
type RestoreProvider interface {
	Provider
	Plan(context.Context, profile.Data, omarchy.Info, RestoreMode) (model.RestorePlan, error)
	Verify(context.Context, profile.Data) (model.VerificationResult, error)
}

// Provider is the narrow shared contract for status and capture orchestration.
// Provider-specific construction remains at the application boundary.
type Provider interface {
	ID() string
	Captured(profile.Data) bool
	Capture(context.Context, *profile.Data) (any, []model.Change, error)
	Diff(context.Context, profile.Data) ([]model.Change, error)
}

type categoryProvider interface {
	Provider
	CategoryEnabled() bool
}

type emptyProvider interface{ Empty(any) bool }

func ProviderIDs(providers []Provider) []string {
	ids := make([]string, 0, len(providers))
	for _, provider := range providers {
		if category, ok := provider.(categoryProvider); ok && category.CategoryEnabled() {
			ids = append(ids, provider.ID())
		}
	}
	return ids
}

func ProviderByID(providers []Provider, id string) (Provider, bool) {
	for _, provider := range providers {
		if provider.ID() == id {
			return provider, true
		}
	}
	return nil, false
}

func capturedProviders(providers []Provider, data profile.Data) []Provider {
	selected := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if provider.Captured(data) {
			selected = append(selected, provider)
		}
	}
	return selected
}
