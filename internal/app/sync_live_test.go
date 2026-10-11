package app

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncPackagesSharesObservationWithOtherReadProjections(t *testing.T) {
	p, d, runner, _ := packageReadFixture(t)
	d.Manifest.Capture.Packages = true
	d.Packages.Absent = []profile.PackageAbsence{{Ref: "aur:gone"}}
	cycle := observation.New(context.Background())
	defer cycle.Close()
	view := p.BindReadCycle(cycle)
	observer, ok := view.(workflow.SyncLiveProvider)
	if !ok {
		t.Fatal("missing read-bound Sync projection")
	}
	facts, err := observer.SyncObserve(context.Background(), d, []profile.Data{d})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]syncengine.State{}
	for _, x := range facts.Targets {
		states[x.Key.Target] = x.State
	}
	want, _ := p.SyncDesired(d)
	for _, x := range want {
		if x.Key.Target == "official:git" || x.Key.Target == "aur:gone" {
			if !x.State.Equal(states[x.Key.Target]) {
				t.Fatal("desired/live normalization mismatch", x, states)
			}
		}
	}
	if states["official:missing"].Kind != syncengine.StateAbsent {
		t.Fatal("missing managed package mistaken for present")
	}
	if _, err := view.Diff(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if _, err := view.InspectTargets(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if runner.detections != 1 {
		t.Fatal("Sync duplicated Packages.Detect", runner.detections)
	}
}

func TestSyncOtherProviderReadProjections(t *testing.T) {
	dir, deps := configSandbox(t)
	_, user, err := deps.ConfigDirs()
	if err != nil {
		t.Fatal(err)
	}
	deps.HomeDir = func() (string, error) { return filepath.Dir(user), nil }
	plugins, hooks := t.TempDir(), t.TempDir()
	deps.PluginDir = func() (string, error) { return plugins, nil }
	deps.HooksDir = func() (string, error) { return hooks, nil }
	deps.MiseGlobalConfig = func() (string, error) { return "", nil }
	deps.ResourceLinkRoots = func(string) []resourcesprovider.LinkSearchRoot { return nil }
	d := profile.New("test", time.Time{})
	d.Manifest.Capture = profile.CaptureMeta{Packages: true, Themes: true, Plugins: true, Config: true, Defaults: true, Shell: true, Hooks: true, Resources: true}
	for _, p := range workflowProviders(deps, &options{profileDir: dir}) {
		if p.ID() == "services" || p.ID() == "packages" {
			continue
		}
		cycle := observation.New(context.Background())
		view := p.(workflow.ReadCycleBinder).BindReadCycle(cycle)
		observer, ok := view.(workflow.SyncLiveProvider)
		if !ok { // Narrow forwarding deliberately hides authority; it can expose
			// Sync through a read-only adapter without granting Capture/Verify.
			observer, ok = p.(workflow.SyncLiveProvider)
		}
		if !ok {
			cycle.Close()
			t.Fatalf("%s lacks Sync observation", p.ID())
		}
		facts, err := observer.SyncObserve(context.Background(), d, []profile.Data{d})
		cycle.Close()
		if err != nil {
			t.Fatalf("%s: %v", p.ID(), err)
		}
		inspections := map[string]bool{}
		for _, x := range facts.Inspections {
			inspections[x.Key] = true
		}
		for _, x := range facts.Targets {
			if !inspections[x.Key.Target] || x.Key.Category != p.ID() {
				t.Fatal("live identity mismatch", x)
			}
		}
	}
}

func TestSyncLiveMissingIsNotAlwaysDesiredAbsent(t *testing.T) {
	key := syncengine.TargetKey{Category: "defaults", Target: "browser"}
	present := syncengine.DesiredTarget{Key: key, State: syncengine.State{Kind: syncengine.StatePresent, Fingerprint: "value"}}
	facts, err := syncLiveFacts("defaults", nil, []workflow.TargetInspection{{Key: "browser", Current: workflow.TargetAbsent}}, [][]syncengine.DesiredTarget{{present}})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Targets) != 1 || facts.Targets[0].State.Kind != syncengine.StateUnmanaged {
		t.Fatal("provider without tombstones manufactured absence", facts.Targets)
	}
}

func TestSyncLiveBaselineCustomizationIsUnmanagedAndNewDeletionIsIntent(t *testing.T) {
	key := syncengine.TargetKey{Category: "config", Target: ".config/demo"}
	old := syncengine.DesiredTarget{Key: key, State: syncengine.State{Kind: syncengine.StatePresent, Fingerprint: "old"}}
	facts, err := syncLiveFacts("config", nil, []workflow.TargetInspection{{Key: key.Target, Current: workflow.TargetPresent, Capabilities: workflow.TargetCapabilities{NoActionableUpdate: true}}}, [][]syncengine.DesiredTarget{{old}})
	if err != nil || facts.Targets[0].State.Kind != syncengine.StateUnmanaged {
		t.Fatal("baseline no-customization became unknown/present", facts, err)
	}
	gone := syncengine.DesiredTarget{Key: key, State: syncengine.State{Kind: syncengine.StateAbsent, Fingerprint: "baseline-provenance"}}
	facts, err = syncLiveFacts("config", []syncengine.DesiredTarget{gone}, []workflow.TargetInspection{{Key: key.Target, Current: workflow.TargetAbsent, Capabilities: workflow.TargetCapabilities{SupportsDesiredAbsence: true, RecordsNewAbsence: true}}}, [][]syncengine.DesiredTarget{{}})
	if err != nil || !facts.Targets[0].State.Equal(gone.State) {
		t.Fatal("new deletion intent collapsed to unmanaged", facts, err)
	}
}
