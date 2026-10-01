package services

import (
	"context"
	"testing"
	"time"

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
	if fragment, err := p.Plan(context.Background(), profile.New("empty", time.Unix(1, 0)), omarchy.Info{}, workflow.RestoreContext{}); err != nil || len(fragment.Operations) != 0 || fragment.Compatibility.Applies {
		t.Fatalf("empty Services Restore gained authority: %+v err=%v", fragment, err)
	}
}
