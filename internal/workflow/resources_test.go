package workflow

import (
	"context"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type resourceActionTestProvider struct{ data profile.Data }

func (p *resourceActionTestProvider) ID() string                 { return "resources" }
func (p *resourceActionTestProvider) Captured(profile.Data) bool { return true }
func (p *resourceActionTestProvider) Capture(context.Context, *profile.Data) (any, []model.Change, error) {
	return nil, nil, nil
}
func (p *resourceActionTestProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return nil, nil
}
func (p *resourceActionTestProvider) TrackResource(_ context.Context, d profile.Data, request TrackRequest) (profile.Data, profile.Resource, []model.Change, error) {
	resource := profile.Resource{ID: request.ID, Path: "~/Projects", Strategy: request.Strategy}
	d.Resources.Items = append(d.Resources.Items, resource)
	return d, resource, []model.Change{{Provider: "resources", Kind: "resource", Name: resource.ID}}, nil
}
func (p *resourceActionTestProvider) UntrackResource(_ context.Context, d profile.Data, id string) (profile.Data, []string, error) {
	d.Resources.Items = nil
	return d, []string{"resource:" + id}, nil
}

func TestTrackResourceUpdatesSessionProfile(t *testing.T) {
	p := &resourceActionTestProvider{}
	s := &Session{providers: []Provider{p}}
	resource, changes, err := s.TrackResource(context.Background(), TrackRequest{Path: "/tmp/projects", ID: "projects", Strategy: "copy"})
	if err != nil || resource.ID != "projects" || len(changes) != 1 || len(s.Profile().Resources.Items) != 1 {
		t.Fatalf("resource=%#v changes=%#v profile=%#v err=%v", resource, changes, s.Profile().Resources, err)
	}
}

func TestUntrackResourceKeepsWorkflowContract(t *testing.T) {
	p := &resourceActionTestProvider{}
	s := &Session{profile: profile.Data{Resources: profile.Resources{Items: []profile.Resource{{ID: "projects"}}}}, providers: []Provider{p}}
	removed, err := s.UntrackResource(context.Background(), "projects")
	if err != nil || len(removed) != 1 || removed[0] != "resource:projects" || len(s.Profile().Resources.Items) != 0 {
		t.Fatalf("removed=%v profile=%#v err=%v", removed, s.Profile().Resources, err)
	}
}
