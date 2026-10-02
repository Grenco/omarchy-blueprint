package services

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type countingReadSystemd struct {
	Systemd
	calls atomic.Int32
	units []ObservedUnit
	err   error
}

func (s *countingReadSystemd) InspectUserUnits(context.Context) ([]ObservedUnit, error) {
	s.calls.Add(1)
	return s.units, s.err
}

func TestServicesReadViewObservesOnceForDiffTargetsAndPlan(t *testing.T) {
	p, data, sd, _ := persistentPlanFixture(t)
	counter := &countingReadSystemd{Systemd: sd, units: sd.units}
	p.Systemd = counter
	ctx := context.Background()
	wantDiff, e := p.Diff(ctx, *data)
	if e != nil {
		t.Fatal(e)
	}
	wantTargets, e := p.InspectTargets(ctx, *data)
	if e != nil {
		t.Fatal(e)
	}
	wantPlan, e := p.Plan(ctx, *data, omarchy.Info{}, planContext(*data, false, false))
	if e != nil {
		t.Fatal(e)
	}
	counter.calls.Store(0)
	c := observation.New(ctx)
	defer c.Close()
	view := p.BindReadCycle(c)
	gotDiff, e := view.Diff(ctx, *data)
	if e != nil {
		t.Fatal(e)
	}
	gotTargets, e := view.InspectTargets(ctx, *data)
	if e != nil {
		t.Fatal(e)
	}
	gotPlan, e := view.(workflow.ReadRestoreProvider).Plan(ctx, *data, omarchy.Info{}, planContext(*data, false, false))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(gotDiff, wantDiff) || !reflect.DeepEqual(gotTargets, wantTargets) || !reflect.DeepEqual(gotPlan, wantPlan) {
		t.Fatal("read/fresh projection mismatch")
	}
	if counter.calls.Load() != 1 {
		t.Fatalf("inventories=%d", counter.calls.Load())
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := view.Diff(ctx, *data); e != nil {
				t.Error(e)
			}
			if _, e := view.InspectTargets(ctx, *data); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if counter.calls.Load() != 1 {
		t.Fatal("concurrent projections duplicated inventory")
	}
}

func TestServicesReadViewHasNoMutationCapabilities(t *testing.T) {
	c := observation.New(context.Background())
	defer c.Close()
	v := (&Provider{}).BindReadCycle(c)
	if _, ok := v.(workflow.Provider); ok {
		t.Fatal("read view grants Capture")
	}
	if _, ok := v.(workflow.RestoreProvider); ok {
		t.Fatal("read view grants Verify")
	}
	if _, ok := v.(interface{ CommitCapture() error }); ok {
		t.Fatal("read view grants capture transaction")
	}
}

func TestServicesReadRootsDoNotMutateProvider(t *testing.T) {
	original := Roots{UserConfigDir: "/original"}
	resolved := Roots{UserConfigDir: "/resolved"}
	sd := &countingReadSystemd{units: []ObservedUnit{observedService("external.service", "/external")}}
	p := &Provider{Roots: original, ResolveRoots: func() (Roots, error) { return resolved, nil }, Systemd: sd}
	ctx := context.Background()
	data := profile.Data{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := observation.New(ctx)
			defer c.Close()
			v := p.BindReadCycle(c)
			if _, e := v.InspectTargets(ctx, data); e != nil {
				t.Error(e)
			}
			if _, e := v.Diff(ctx, data); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if p.Roots != original {
		t.Fatal("read changed provider roots")
	}
	if sd.calls.Load() != 8 {
		t.Fatalf("loads=%d", sd.calls.Load())
	}
	if _, e := p.InspectTargets(ctx, data); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Diff(ctx, data); e != nil {
		t.Fatal(e)
	}
	if p.Roots != original {
		t.Fatal("fresh read changed provider roots")
	}
}

func TestServicesObservationCopiesNestedUnitsAndRoots(t *testing.T) {
	source := Observation{Roots: Roots{UserConfigDir: "/root"}, Units: []ObservedUnit{{Name: "one.service", DropInPaths: []string{"drop"}, RelatedUnits: []string{"related"}, TriggeredBy: []string{"timer"}}}}
	got := source.Clone()
	got.Units[0].Name = "other.service"
	got.Units[0].DropInPaths[0] = "changed"
	got.Units[0].RelatedUnits[0] = "changed"
	got.Units[0].TriggeredBy[0] = "changed"
	got.Roots.UserConfigDir = "/changed"
	if source.Roots.UserConfigDir != "/root" || source.Units[0].Name != "one.service" || source.Units[0].DropInPaths[0] != "drop" || source.Units[0].RelatedUnits[0] != "related" || source.Units[0].TriggeredBy[0] != "timer" {
		t.Fatal("copy aliases source")
	}
}

func TestServicesIndependentCyclesResolveRootsAgain(t *testing.T) {
	var resolutions atomic.Int32
	p := &Provider{Systemd: &countingReadSystemd{}, Roots: Roots{UserConfigDir: "/original"}, ResolveRoots: func() (Roots, error) {
		if resolutions.Add(1) == 1 {
			return Roots{UserConfigDir: "/first"}, nil
		}
		return Roots{UserConfigDir: "/second"}, nil
	}}
	for _, expected := range []string{"/first", "/second"} {
		c := observation.New(context.Background())
		v := p.BindReadCycle(c).(*readView)
		o, e := v.slot.Get(context.Background())
		if e != nil || o.Roots.UserConfigDir != expected {
			t.Fatalf("%v %v", o.Roots, e)
		}
		c.Close()
	}
	if p.Roots.UserConfigDir != "/original" {
		t.Fatal("roots leaked into provider")
	}
}

func TestServicesFreshPlanAndVerifyRootsAreLocal(t *testing.T) {
	p, data, sd, _ := persistentPlanFixture(t)
	actual := p.Roots
	p.Roots = Roots{UserConfigDir: "/not-resolved"}
	p.ResolveRoots = func() (Roots, error) { return actual, nil }
	counter := &countingReadSystemd{Systemd: sd, units: sd.units}
	p.Systemd = counter
	ctx := context.Background()
	rc := planContext(*data, false, false)
	if _, e := p.Plan(ctx, *data, omarchy.Info{}, rc); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Verify(ctx, *data, rc); e != nil {
		t.Fatal(e)
	}
	if counter.calls.Load() != 2 || p.Roots.UserConfigDir != "/not-resolved" {
		t.Fatal("authority did not inspect freshly with local roots")
	}
}

func TestServicesVerifyDoesNotUseReadSnapshot(t *testing.T) {
	p, data, sd, _ := persistentPlanFixture(t)
	counter := &countingReadSystemd{Systemd: sd, units: sd.units}
	p.Systemd = counter
	ctx := context.Background()
	c := observation.New(ctx)
	defer c.Close()
	v := p.BindReadCycle(c)
	if _, e := v.Diff(ctx, *data); e != nil {
		t.Fatal(e)
	}
	counter.units = nil
	result, e := p.Verify(ctx, *data, planContext(*data, false, false))
	if e != nil {
		t.Fatal(e)
	}
	if result.OK || counter.calls.Load() != 2 {
		t.Fatal("Verify trusted read inventory A after unit disappeared")
	}
	if _, e := v.Diff(ctx, *data); e != nil {
		t.Fatal(e)
	}
	if counter.calls.Load() != 2 {
		t.Fatal("fresh Verify contaminated cycle")
	}
}

func TestServicesReadUnavailableManagerProjections(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "saved"}[saved], func(t *testing.T) {
			p, data, sd, _ := persistentPlanFixture(t)
			if !saved {
				data.Services = profile.Services{}
			}
			counter := &countingReadSystemd{Systemd: sd, err: errors.New("manager unavailable")}
			p.Systemd = counter
			c := observation.New(context.Background())
			defer c.Close()
			v := p.BindReadCycle(c)
			ctx := context.Background()
			_, diffErr := v.Diff(ctx, *data)
			if (diffErr != nil) != saved {
				t.Fatalf("diff error=%v", diffErr)
			}
			if targets, e := v.InspectTargets(ctx, *data); e != nil || len(targets) != 1 {
				t.Fatalf("targets=%v error=%v", targets, e)
			}
			if _, e := v.(workflow.ReadRestoreProvider).Plan(ctx, *data, omarchy.Info{}, planContext(*data, false, false)); e != nil {
				t.Fatal(e)
			}
			if counter.calls.Load() != 1 {
				t.Fatal(counter.calls.Load())
			}
		})
	}
}

func TestServicesReadPlanWithNoSelectionDoesNotObserve(t *testing.T) {
	p, data, sd, _ := persistentPlanFixture(t)
	counter := &countingReadSystemd{Systemd: sd, units: sd.units}
	p.Systemd = counter
	c := observation.New(context.Background())
	defer c.Close()
	v := p.BindReadCycle(c)
	rc := planContext(*data, false, false)
	for key := range rc.Targets {
		rc.Targets[key] = workflow.RestoreDecision{Resolved: true}
	}
	if _, e := v.(workflow.ReadRestoreProvider).Plan(context.Background(), *data, omarchy.Info{}, rc); e != nil {
		t.Fatal(e)
	}
	if counter.calls.Load() != 0 {
		t.Fatal("empty selection probed manager")
	}
}
