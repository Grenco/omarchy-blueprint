package services

import (
	"context"
	"errors"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

var _ workflow.RestoreProvider = (*Provider)(nil)

func TestServicesProviderSkeletonFailsClosedUntilDiscoveryAndRestore(t *testing.T) {
	p := Provider{}
	if p.ID() != "services" || !p.CategoryEnabled() || p.Captured(profile.Data{}) {
		t.Fatalf("premature Services authority: %+v", p)
	}
	if _, err := p.InspectTargets(context.Background(), profile.Data{}); err == nil {
		t.Fatal("Services inspection accepted a missing user systemd source")
	}
	if _, err := p.Plan(context.Background(), profile.Data{}, omarchy.Info{}, workflow.RestoreContext{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("unimplemented Services Restore planned anything: %v", err)
	}
}
