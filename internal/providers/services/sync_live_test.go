package services

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncServicesSharesInventoryAndPersistentNormalization(t *testing.T) {
	p, data, sd, _ := persistentPlanFixture(t)
	counter := &countingReadSystemd{Systemd: sd, units: sd.units}
	p.Systemd = counter
	cycle := observation.New(context.Background())
	defer cycle.Close()
	v := p.BindReadCycle(cycle)
	observer, ok := v.(workflow.SyncLiveProvider)
	if !ok {
		t.Fatal("missing read-bound Services Sync projection")
	}
	facts, err := observer.SyncObserve(context.Background(), *data, []profile.Data{*data})
	if err != nil {
		t.Fatal(err)
	}
	desired, err := p.SyncDesired(*data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range desired {
		found := false
		for _, got := range facts.Targets {
			if got.Key == want.Key && got.State.Equal(want.State) {
				found = true
			}
		}
		if !found {
			t.Fatal("live/desired mismatch", want, facts.Targets)
		}
	}
	if _, err := v.Diff(context.Background(), *data); err != nil {
		t.Fatal(err)
	}
	if _, err := v.InspectTargets(context.Background(), *data); err != nil {
		t.Fatal(err)
	}
	if counter.calls.Load() != 1 {
		t.Fatal("repeated service inventory", counter.calls.Load())
	}
	// Active runtime drift cannot create persistent-only semantic drift.
	data.Services.Units[0].ObservedActive = !data.Services.Units[0].ObservedActive
	facts, err = observer.SyncObserve(context.Background(), *data, []profile.Data{*data})
	if err != nil {
		t.Fatal(err)
	}
	desired, _ = p.SyncDesired(*data)
	for _, want := range desired {
		found := false
		for _, got := range facts.Targets {
			if got.Key == want.Key && got.State.Equal(want.State) {
				found = true
			}
		}
		if !found {
			t.Fatal("volatile runtime affected intent")
		}
	}
}

func TestSyncServiceArtifactUnreadableIsNotAbsence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "demo.conf")
	if err := os.Symlink("/dev/null", path); err != nil {
		t.Fatal(err)
	}
	a := profile.ServiceArtifact{Presence: profile.ServicePresent, Hash: "prior"}
	if _, err := observeSyncDropIn(a, path); err == nil {
		t.Fatal("unsafe artifact became desired absence")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got, err := observeSyncDropIn(a, path)
	if err != nil || got.Presence != profile.ServiceAbsent || got.Hash != "prior" {
		t.Fatal("missing artifact lost provenance", got, err)
	}
}
