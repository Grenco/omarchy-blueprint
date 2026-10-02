package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type cycleExpensiveProvider struct {
	id                            string
	live, loads, plans, mutations atomic.Int32
}

func (p *cycleExpensiveProvider) ID() string               { return p.id }
func (*cycleExpensiveProvider) CategoryEnabled() bool      { return true }
func (*cycleExpensiveProvider) Captured(profile.Data) bool { return true }
func (p *cycleExpensiveProvider) observe(context.Context) (int, error) {
	p.loads.Add(1)
	return int(p.live.Load()), nil
}
func (p *cycleExpensiveProvider) key() string {
	if p.id == "services" {
		return "backup.service"
	}
	return "official:git"
}
func (p *cycleExpensiveProvider) targets(value int) []TargetInspection {
	return []TargetInspection{{Key: p.key(), Label: p.key(), Desired: TargetPresent, Current: TargetPresent, CaptureEligible: true, RestoreEligible: true, Fingerprint: fmt.Sprint(value), Capabilities: TargetCapabilities{SupportsCapture: true, SupportsRestore: true}}}
}
func (p *cycleExpensiveProvider) diff(value int) []model.Change {
	return []model.Change{{Provider: p.id, Kind: "state", Name: p.key(), Summary: fmt.Sprint(value), Type: model.ChangeModify}}
}
func (p *cycleExpensiveProvider) plan(value int, rc RestoreContext) RestoreFragment {
	p.plans.Add(1)
	category := model.CompatibilityCategory{Category: p.id, Applies: rc.Applies(), Authority: model.CompatibilityUnchanged}
	if category.Applies {
		category.State = model.CompatibilitySupported
		category.Evidence = []model.CompatibilityEvidence{{Kind: "test", Summary: "supported"}}
	}
	return RestoreFragment{Skipped: []model.Skipped{{Provider: p.id, Resource: p.key(), Reason: fmt.Sprint(value)}}, Compatibility: category}
}
func (p *cycleExpensiveProvider) InspectTargets(ctx context.Context, _ profile.Data) ([]TargetInspection, error) {
	v, e := p.observe(ctx)
	return p.targets(v), e
}
func (p *cycleExpensiveProvider) Diff(ctx context.Context, _ profile.Data) ([]model.Change, error) {
	v, e := p.observe(ctx)
	return p.diff(v), e
}
func (p *cycleExpensiveProvider) Plan(ctx context.Context, _ profile.Data, _ omarchy.Info, rc RestoreContext) (RestoreFragment, error) {
	v, e := p.observe(ctx)
	return p.plan(v, rc), e
}
func (p *cycleExpensiveProvider) Capture(context.Context, *profile.Data, CaptureContext) (any, []model.Change, error) {
	p.mutations.Add(1)
	return nil, nil, nil
}
func (p *cycleExpensiveProvider) Verify(context.Context, profile.Data, RestoreContext) (model.VerificationResult, error) {
	p.mutations.Add(1)
	return model.VerificationResult{OK: true}, nil
}
func (p *cycleExpensiveProvider) ChangeTargetKey(c model.Change) (string, bool) { return c.Name, true }
func (p *cycleExpensiveProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return []string{op.Resource}, true
}
func (p *cycleExpensiveProvider) BindReadCycle(c *observation.Cycle) ReadProvider {
	return &cycleBoundProvider{p: p, slot: observation.NewSlot(c, p.observe, func(v int) int { return v })}
}

type cycleBoundProvider struct {
	p    *cycleExpensiveProvider
	slot *observation.Slot[int]
}

func (p *cycleBoundProvider) ID() string               { return p.p.ID() }
func (*cycleBoundProvider) CategoryEnabled() bool      { return true }
func (*cycleBoundProvider) Captured(profile.Data) bool { return true }
func (p *cycleBoundProvider) InspectTargets(ctx context.Context, _ profile.Data) ([]TargetInspection, error) {
	v, e := p.slot.Get(ctx)
	return p.p.targets(v), e
}
func (p *cycleBoundProvider) Diff(ctx context.Context, _ profile.Data) ([]model.Change, error) {
	v, e := p.slot.Get(ctx)
	return p.p.diff(v), e
}
func (p *cycleBoundProvider) Plan(ctx context.Context, _ profile.Data, _ omarchy.Info, rc RestoreContext) (RestoreFragment, error) {
	v, e := p.slot.Get(ctx)
	return p.p.plan(v, rc), e
}
func (p *cycleBoundProvider) ChangeTargetKey(c model.Change) (string, bool) {
	return p.p.ChangeTargetKey(c)
}
func (p *cycleBoundProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return p.p.RestoreOperationTargetKeys(op)
}

func cycleSession(t *testing.T, p *cycleExpensiveProvider, preserve bool) *Session {
	t.Helper()
	data := profile.New("cycle", time.Unix(1, 0))
	if preserve {
		data.Policy.Capture = []policy.Rule{{Category: p.id, Setting: policy.SettingDisabled}}
	}
	s := newCaptureSession(t, data)
	s.deps.Runner = planRunner{}
	if e := s.SetProviders([]Provider{p}); e != nil {
		t.Fatal(e)
	}
	p.live.Store(1)
	return s
}

func TestReadCycleSnapshotUnaffectedByReloadAndProfileEdits(t *testing.T) {
	p := &cycleExpensiveProvider{id: "packages"}
	s := cycleSession(t, p, false)
	ctx := context.Background()
	c, e := s.BeginRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	first, e := c.Status(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	edited := profile.CloneData(first.Profile)
	edited.Manifest.Profile.Name = "edited"
	edited.Packages.Official = []string{"changed"}
	if e := profile.Save(s.ProfileDir(), edited); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			if e := s.Reload(); e != nil {
				t.Error(e)
			}
		}
	}()
	for range 20 {
		got, e := c.Status(ctx, "")
		if e != nil || !reflect.DeepEqual(got.Profile, first.Profile) {
			t.Error("existing cycle changed", e)
		}
	}
	wg.Wait()
	fresh, e := s.BeginRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.Close()
	next, e := fresh.Status(ctx, "")
	if e != nil || next.Profile.Manifest.Profile.Name != "edited" {
		t.Fatal("new cycle did not load edit", e)
	}
	next.Profile.Packages.Official[0] = "caller edit"
	nextAgain, e := fresh.Status(ctx, "")
	if e != nil || nextAgain.Profile.Packages.Official[0] != "changed" {
		t.Fatal("caller mutated snapshot", e)
	}
}

func TestOverlappingReadCyclesOwnIndependentSnapshots(t *testing.T) {
	p := &cycleExpensiveProvider{id: "services"}
	s := cycleSession(t, p, false)
	ctx := context.Background()
	a, e := s.BeginRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	ra, e := a.Status(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	p.live.Store(2)
	b, e := s.BeginRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	rb, e := b.Status(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	again, e := a.Status(ctx, "")
	if e != nil || !reflect.DeepEqual(again, ra) || reflect.DeepEqual(ra, rb) {
		t.Fatal("cycles shared facts", e)
	}
	if p.loads.Load() != 2 {
		t.Fatal(p.loads.Load())
	}
}

func TestOverviewReadCycleCaptureShortCircuit(t *testing.T)   { testOverviewCycle(t, false) }
func TestOverviewReadCycleRestoreClassification(t *testing.T) { testOverviewCycle(t, true) }
func testOverviewCycle(t *testing.T, preserve bool) {
	for _, id := range []string{"services", "packages"} {
		t.Run(id, func(t *testing.T) {
			p := &cycleExpensiveProvider{id: id}
			s := cycleSession(t, p, preserve)
			loadState := s.deps.StateHome
			stateLoads := 0
			s.deps.StateHome = func() (string, error) { stateLoads++; return loadState() }
			if _, e := s.Overview(context.Background()); e != nil {
				t.Fatal(e)
			}
			if p.loads.Load() != 1 {
				t.Fatalf("observations=%d", p.loads.Load())
			}
			if stateLoads != 1 {
				t.Fatalf("classification reloaded desired state %d times", stateLoads)
			}
			expected := int32(0)
			if preserve {
				expected = 1
			}
			if p.plans.Load() != expected {
				t.Fatalf("plans=%d expected=%d", p.plans.Load(), expected)
			}
		})
	}
}

func TestCapturePreviewManySharesCycle(t *testing.T) {
	p := &cycleExpensiveProvider{id: "services"}
	s := cycleSession(t, p, false)
	ctx := context.Background()
	c, e := s.BeginRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if _, e := c.Status(ctx, ""); e != nil {
		t.Fatal(e)
	}
	if _, e := c.InspectCaptureMany(ctx, []string{p.id}); e != nil {
		t.Fatal(e)
	}
	if p.loads.Load() != 1 {
		t.Fatal(p.loads.Load())
	}
}

func TestRestorePreviewSafeAndForceShareObservation(t *testing.T) {
	p := &cycleExpensiveProvider{id: "packages"}
	s := cycleSession(t, p, false)
	ctx := context.Background()
	c, e := s.BeginRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	safe := policy.DefaultRestoreOptions()
	force := safe
	force.Conflicts = policy.ConflictForce
	a, e := c.PreviewRestore(ctx, "", &safe)
	if e != nil {
		t.Fatal(e)
	}
	b, e := c.PreviewRestore(ctx, "", &force)
	if e != nil {
		t.Fatal(e)
	}
	if p.loads.Load() != 1 || a.Options != safe || b.Options != force {
		t.Fatal("preview did not share facts/retain options")
	}
	a.Plan.Skipped[0].Reason = "changed"
	again, e := c.PreviewRestore(ctx, "", &safe)
	if e != nil || again.Plan.Skipped[0].Reason != "1" {
		t.Fatal("caller changed cycle", e)
	}
	c.Close()
	if _, e := c.Status(ctx, ""); !errors.Is(e, observation.ErrCycleClosed) {
		t.Fatal("closed cycle accepted projection", e)
	}
}

type sharedPreviewProvider struct {
	*cycleExpensiveProvider
	part RestoreFragment
}

func (p *sharedPreviewProvider) Plan(context.Context, profile.Data, omarchy.Info, RestoreContext) (RestoreFragment, error) {
	return p.part, nil
}

// Do not bind the fixture's overridden Plan through its embedded binder.
func (p *sharedPreviewProvider) BindReadCycle(*observation.Cycle) ReadProvider {
	return NarrowReadProvider(p)
}
func TestReadPreviewCopiesProviderDerivedCollections(t *testing.T) {
	p := &cycleExpensiveProvider{id: "packages"}
	s := cycleSession(t, p, false)
	part := p.plan(1, RestoreContext{Targets: map[string]RestoreDecision{p.key(): {Resolved: true, Restore: true}}})
	part.Operations = []model.Operation{{ID: "packages.test", Provider: "packages", Resource: p.key(), Action: "install", Command: []string{"pacman", "-S", "git"}, Risk: model.RiskLow}}
	shared := &sharedPreviewProvider{cycleExpensiveProvider: p, part: part}
	if e := s.SetProviders([]Provider{shared}); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	c, e := s.BeginRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	first, e := c.PreviewRestore(ctx, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	first.Plan.Compatibility.Categories[0].Evidence[0].Summary = "caller edit"
	first.Plan.Operations[0].Command[2] = "caller edit"
	again, e := c.PreviewRestore(ctx, "", nil)
	if e != nil || again.Plan.Compatibility.Categories[0].Evidence[0].Summary != "supported" {
		t.Fatal("caller edited provider-derived state", e)
	}
	if again.Plan.Operations[0].Command[2] != "git" {
		t.Fatal("caller edited provider operation")
	}
}

type blockingReadProvider struct {
	*cycleExpensiveProvider
	started    chan struct{}
	probeError chan error
}

func (p *blockingReadProvider) BindReadCycle(*observation.Cycle) ReadProvider {
	return NarrowReadProvider(p)
}
func (p *blockingReadProvider) Diff(ctx context.Context, _ profile.Data) ([]model.Change, error) {
	close(p.started)
	<-ctx.Done()
	p.probeError <- ctx.Err()
	return nil, ctx.Err()
}
func TestReadCycleParentCancellationReachesForwardedProbe(t *testing.T) {
	p := &cycleExpensiveProvider{id: "packages"}
	s := cycleSession(t, p, false)
	block := &blockingReadProvider{cycleExpensiveProvider: p, started: make(chan struct{}), probeError: make(chan error, 1)}
	if e := s.SetProviders([]Provider{block}); e != nil {
		t.Fatal(e)
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, e := s.BeginRead(parent)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	waiter, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	result := make(chan error, 1)
	go func() { _, e := c.Status(waiter, ""); result <- e }()
	<-block.started
	cancel()
	if probeErr := <-block.probeError; !errors.Is(probeErr, context.Canceled) {
		t.Fatalf("probe saw %v instead of parent cancellation", probeErr)
	}
	if e := <-result; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestReadCycleLoadsBindingAndMachineDefaultsPrivately(t *testing.T) {
	p := &cycleExpensiveProvider{id: "packages"}
	s := cycleSession(t, p, false)
	data := s.Profile()
	data.Machines.Items = []profile.Machine{{Name: "first"}, {Name: "second", RestoreConflicts: policy.ConflictForce, Policy: policy.Rules{Capture: []policy.Rule{{Category: "packages", Setting: policy.SettingDisabled}}}}}
	if e := profile.Save(s.ProfileDir(), data); e != nil {
		t.Fatal(e)
	}
	state, e := s.deps.StateHome()
	if e != nil {
		t.Fatal(e)
	}
	store := machine.BindingStore{StateHome: state}
	if e := store.Save(s.ProfileDir(), "first"); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	a, e := s.BeginRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if e := store.Save(s.ProfileDir(), "second"); e != nil {
		t.Fatal(e)
	}
	b, e := s.BeginRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	first, e := a.PreviewRestore(ctx, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	second, e := b.PreviewRestore(ctx, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	if first.Machine.Name != "first" || second.Machine.Name != "second" || first.Options.Conflicts != policy.ConflictSafe || second.Options.Conflicts != policy.ConflictForce {
		t.Fatal("binding/defaults snapshots mixed")
	}
	if s.Machine().Name != "" {
		t.Fatal("read cycle published machine selection into Session")
	}
	second.Machine.Machine.Policy.Capture[0].Category = "caller edit"
	again, e := b.PreviewRestore(ctx, "", nil)
	if e != nil || again.Machine.Machine.Policy.Capture[0].Category != "packages" {
		t.Fatal("returned machine aliased cycle", e)
	}
}

func TestReadCycleProviderRegistryIsCaptured(t *testing.T) {
	p := &cycleExpensiveProvider{id: "packages"}
	s := cycleSession(t, p, false)
	ctx := context.Background()
	a, e := s.BeginRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if e := s.SetProviders([]Provider{&cycleExpensiveProvider{id: "services"}}); e != nil {
		t.Fatal(e)
	}
	first, e := a.Status(ctx, "")
	if e != nil || len(first.Providers) != 1 || first.Providers[0].ID != "packages" {
		t.Fatal("registry changed existing cycle", e)
	}
	next, e := s.Status(ctx, "")
	if e != nil || len(next.Providers) != 1 || next.Providers[0].ID != "services" {
		t.Fatal("new cycle did not capture registry", e)
	}
}
