package workflow

import (
	"context"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// RestoreProvider extends a provider with restore planning and verification.
type RestoreProvider interface {
	Provider
	Plan(context.Context, profile.Data, omarchy.Info, RestoreContext) (RestoreFragment, error)
	Verify(context.Context, profile.Data, RestoreContext) (model.VerificationResult, error)
}

// RestoreFragment keeps one provider's effects and compatibility derived from
// the same effective intent and detected target state.
type RestoreFragment struct {
	ActivationReview []model.ActivationCandidate
	Operations       []model.Operation
	Skipped          []model.Skipped
	Requirements     []model.Requirement
	Compatibility    model.CompatibilityCategory
}

// Provider is the narrow shared contract for status and capture orchestration.
// Provider-specific construction remains at the application boundary.
type ReadProvider interface {
	ID() string
	Captured(profile.Data) bool
	// InspectTargets returns the provider's read-only, descriptive policy
	// targets. It must not mutate state; Capture and Plan/Verify re-inspect
	// before mutation rather than trusting a stale inventory.
	InspectTargets(context.Context, profile.Data) ([]TargetInspection, error)
	Diff(context.Context, profile.Data) ([]model.Change, error)
}

// ReadRestoreProvider derives previews but grants no execution or verification.
type ReadRestoreProvider interface {
	ReadProvider
	Plan(context.Context, profile.Data, omarchy.Info, RestoreContext) (RestoreFragment, error)
}

type ReadCycleBinder interface {
	BindReadCycle(*observation.Cycle) ReadProvider
}

// Provider retains authoritative Capture separately from its read contract.
type Provider interface {
	ReadProvider
	Capture(context.Context, *profile.Data, CaptureContext) (any, []model.Change, error)
}

type categoryProvider interface {
	CategoryEnabled() bool
}

type emptyProvider interface{ Empty(any) bool }

func ProviderIDs[T ReadProvider](providers []T) []string {
	ids := make([]string, 0, len(providers))
	for _, provider := range providers {
		if enabled, ok := readCategoryState(provider); ok && enabled {
			ids = append(ids, provider.ID())
		}
	}
	return ids
}

func ProviderByID[T ReadProvider](providers []T, id string) (T, bool) {
	for _, provider := range providers {
		if provider.ID() == id {
			return provider, true
		}
	}
	var zero T
	return zero, false
}

func capturedProviders[T ReadProvider](providers []T, data profile.Data) []T {
	selected := make([]T, 0, len(providers))
	for _, provider := range providers {
		if provider.Captured(profile.CloneData(data)) {
			selected = append(selected, provider)
		}
	}
	return selected
}
