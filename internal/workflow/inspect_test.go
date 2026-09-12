package workflow

import (
	"context"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
)

type inspectTestProvider struct{ id string }

func (p inspectTestProvider) ID() string               { return p.id }
func (inspectTestProvider) Captured(profile.Data) bool { return true }
func (inspectTestProvider) Capture(context.Context, *profile.Data) (any, []model.Change, error) {
	return nil, nil, nil
}
func (inspectTestProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return nil, nil
}
func (inspectTestProvider) InspectPath(_ context.Context, _ profile.Data, path string) (PathInspection, error) {
	return PathInspection{Path: path, Type: "file"}, nil
}
func (inspectTestProvider) InspectResource(_ context.Context, _ profile.Data, id string) (ResourceInspection, error) {
	return ResourceInspection{Resource: profile.Resource{ID: id}}, nil
}
func (inspectTestProvider) InspectConfig(_ context.Context, _ profile.Data, path string) (ConfigInspection, error) {
	return ConfigInspection{Candidate: configprovider.Candidate{Path: path}}, nil
}

func TestSessionInspectDispatchesToProviders(t *testing.T) {
	s := &Session{providers: []Provider{inspectTestProvider{id: "resources"}, inspectTestProvider{id: "config"}}}
	path, err := s.InspectPath(context.Background(), "/tmp/file")
	if err != nil || path.Path != "/tmp/file" {
		t.Fatalf("path=%#v err=%v", path, err)
	}
	config, err := s.InspectConfig(context.Background(), ".config/foo/bar")
	if err != nil || config.Candidate.Path != ".config/foo/bar" {
		t.Fatalf("config=%#v err=%v", config, err)
	}
	resource, err := s.InspectResource(context.Background(), "projects")
	if err != nil || resource.Resource.ID != "projects" {
		t.Fatalf("resource=%#v err=%v", resource, err)
	}
}
