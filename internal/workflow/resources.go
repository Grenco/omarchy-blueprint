package workflow

import (
	"context"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// TrackRequest describes the explicit resource state selected by a caller.
type TrackRequest struct {
	Path             string
	ID               string
	Strategy         string
	IncludeUntracked []string
	ExcludeUntracked []string
}

type resourceActions interface {
	TrackResource(context.Context, profile.Data, TrackRequest) (profile.Data, profile.Resource, []model.Change, error)
	UntrackResource(context.Context, profile.Data, string) (profile.Data, []string, error)
}

// TrackResource captures one resource and persists its metadata atomically.
func (s *Session) TrackResource(ctx context.Context, request TrackRequest) (profile.Resource, []model.Change, error) {
	provider, ok := ProviderByID(s.providers, "resources")
	if !ok {
		return profile.Resource{}, nil, fmt.Errorf("resources tracking is unavailable")
	}
	actions, ok := provider.(resourceActions)
	if !ok {
		return profile.Resource{}, nil, fmt.Errorf("resources tracking is unavailable")
	}
	next, resource, changes, err := actions.TrackResource(ctx, s.profile, request)
	if err != nil {
		return profile.Resource{}, nil, err
	}
	s.profile = next
	return resource, changes, nil
}

// UntrackResource removes saved resource metadata without touching its live path.
func (s *Session) UntrackResource(ctx context.Context, id string) ([]string, error) {
	provider, ok := ProviderByID(s.providers, "resources")
	if !ok {
		return nil, fmt.Errorf("resources tracking is unavailable")
	}
	actions, ok := provider.(resourceActions)
	if !ok {
		return nil, fmt.Errorf("resources tracking is unavailable")
	}
	next, removed, err := actions.UntrackResource(ctx, s.profile, id)
	if err != nil {
		return nil, err
	}
	s.profile = next
	return removed, nil
}
