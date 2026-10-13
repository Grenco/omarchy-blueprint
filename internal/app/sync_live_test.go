package app

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	packagesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncDefaultsSafetyFollowsRemoteDesiredValue(t *testing.T) {
	local := profile.Data{}
	local.Manifest.Capture.Defaults = true
	local.Defaults.Browser = "firefox"
	remote := profile.CloneData(local)
	remote.Defaults.Browser = "custom.desktop"
	p := defaultsStateProvider{}
	facts, err := syncDerivedLive("defaults", local, []profile.Data{remote, local}, local, p.SyncDesired, func(d profile.Data) ([]workflow.TargetInspection, error) {
		return targetsFromDefaults(d, local.Defaults)
	})
	if err != nil {
		t.Fatal(err)
	}
	wanted, err := p.SyncDesired(remote)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range wanted {
		if target.Key.Target != "browser" {
			continue
		}
		found := false
		for _, evidence := range facts.DesiredInspections {
			if evidence.Key == target.Key && evidence.Desired.Equal(target.State) {
				found = true
				if evidence.Inspection.RestoreEligible {
					t.Fatal("remote non-portable default inherits local eligibility")
				}
			}
		}
		if !found {
			t.Fatal("missing remote desired safety evidence")
		}
	}
}

func TestSyncPackagesCanonicalAliasesShareOneContext(t *testing.T) {
	p, _, runner, _ := packageReadFixture(t)
	// Canonicalize once from the immutable live catalogue, not per-history
	// metadata or a second machine probe.
	current := profile.Packages{Preinstalls: profile.Preinstalls{Items: map[string]bool{"foo": true}}}
	base := profile.Data{}
	base.Packages.Official = []string{"foo"}
	local := profile.CloneData(base)
	local.Packages.Official = nil
	local.Packages.Absent = []profile.PackageAbsence{{Ref: "aur:foo"}}
	base.Policy.Restore = []policy.Rule{{Category: "packages", Target: "official:foo", Setting: policy.SettingDisabled}, {Category: "packages", Target: "preinstall:foo", Setting: policy.SettingEnabled}}
	base = normalizeSyncPackages(base, current.Preinstalls.Items)
	local = normalizeSyncPackages(local, current.Preinstalls.Items)
	want, err := p.SyncDesired(base)
	if err != nil || len(want) != 1 || want[0].Key.Target != "preinstall:foo" {
		t.Fatal("alias identity lost", want, err)
	}
	absent, err := p.SyncDesired(local)
	if err != nil || len(absent) != 1 || absent[0].State.Kind != syncengine.StateAbsent {
		t.Fatal(absent, err)
	}
	if len(base.Policy.Restore) != 1 || base.Policy.Restore[0].Target != "preinstall:foo" || base.Policy.Restore[0].Setting != policy.SettingEnabled {
		t.Fatal("canonical policy precedence lost")
	}
	facts, err := syncPackagesObserved(base, []profile.Data{base, local}, current)
	if err != nil {
		t.Fatal("canonical live observation failed", err)
	}
	found := false
	for _, x := range facts.Targets {
		if x.Key.Target == "preinstall:foo" {
			found = x.State.Equal(want[0].State)
		}
	}
	if !found {
		t.Fatal("B/L/R/M keyspaces differ", facts.Targets, want)
	}
	b, err := syncprofile.NewBuilder(base)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := (packagesprovider.Provider{}).OverlaySyncTarget(context.Background(), b, syncprofile.View{Profile: local}, "preinstall:foo"); err != nil {
		t.Fatal(err)
	}
	got, _ := p.SyncDesired(b.Profile)
	if len(got) != 1 || !got[0].State.Equal(absent[0].State) {
		t.Fatal("canonical overlay differs", got, absent)
	}
	cycle := observation.New(context.Background())
	defer cycle.Close()
	view := p.BindReadCycle(cycle)
	n, ok := view.(workflow.SyncNormalizeProvider)
	if !ok {
		t.Fatal("missing read-bound identity context")
	}
	if _, err := n.NormalizeSyncProfile(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if _, err := view.Diff(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if runner.detections != 1 {
		t.Fatal("normalization re-probed packages", runner.detections)
	}
}

func TestSyncPackageAliasCanonicalizationRetainsUnmanagedExclusion(t *testing.T) {
	d := profile.Data{}
	d.Packages.Official = []string{"foo"}
	d.Packages.Excluded = []string{"official:foo"}
	d = normalizeSyncPackages(d, map[string]bool{"foo": true})
	got, err := (packagesprovider.Provider{}).SyncDesired(d)
	if err != nil || len(got) != 0 {
		t.Fatal("canonicalization turned excluded alias into managed child", got, err)
	}
}

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
