package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type packageReadRunner struct {
	mu         sync.Mutex
	runner     *machineRunner
	detections int
	responses  map[string]string
}

func (r *packageReadRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if out, ok := r.responses[name+" "+strings.Join(args, " ")]; ok {
		return out, nil
	}
	if name == "pacman" && len(args) == 1 && args[0] == "-Qq" {
		r.detections++
	}
	return r.runner.Run(ctx, name, args...)
}

func packageReadFixture(t *testing.T) (packagesStateProvider, profile.Data, *packageReadRunner, *int) {
	t.Helper()
	_, deps := configSandbox(t)
	resolutions := new(int)
	deps.MiseGlobalConfig = func() (string, error) { *resolutions++; return "", nil }
	runner := &packageReadRunner{runner: deps.Runner.(*machineRunner)}
	deps.Runner = runner
	runner.runner.official = map[string]bool{"git": true, "nvidia-utils": true}
	runner.runner.dependencies = map[string]bool{"dependency": true}
	return packagesStateProvider{deps: deps}, profile.Data{Manifest: profile.Manifest{Schema: profile.Schema}, Packages: profile.Packages{Official: []string{"git", "missing"}}}, runner, resolutions
}
func packageReadContext(targets []workflow.TargetInspection) workflow.RestoreContext {
	rc := workflow.RestoreContext{Options: policy.DefaultRestoreOptions(), Targets: map[string]workflow.RestoreDecision{}}
	for _, target := range targets {
		rc.Targets[target.Key] = workflow.RestoreDecision{Resolved: true, Restore: target.RestoreEligible, CompatibilityApply: target.RestoreEligible}
	}
	return rc
}

func TestPackagesReadViewDetectsOnceForDiffTargetsAndPlan(t *testing.T) {
	p, d, r, resolutions := packageReadFixture(t)
	ctx := context.Background()
	wantDiff, e := p.Diff(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	wantTargets, e := p.InspectTargets(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	rc := packageReadContext(wantTargets)
	wantPlan, e := p.Plan(ctx, d, omarchy.Info{Version: "4.0.0"}, rc)
	if e != nil {
		t.Fatal(e)
	}
	r.detections = 0
	*resolutions = 0
	c := observation.New(ctx)
	defer c.Close()
	v := p.BindReadCycle(c)
	gotDiff, e := v.Diff(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	gotTargets, e := v.InspectTargets(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	gotPlan, e := v.(workflow.ReadRestoreProvider).Plan(ctx, d, omarchy.Info{Version: "4.0.0"}, rc)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(gotDiff, wantDiff) || !reflect.DeepEqual(gotTargets, wantTargets) || !reflect.DeepEqual(gotPlan, wantPlan) {
		t.Fatal("fresh/read projection mismatch")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := v.Diff(ctx, d); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if r.detections != 1 || *resolutions != 1 {
		t.Fatalf("detection=%d resolutions=%d", r.detections, *resolutions)
	}
	if _, ok := v.(workflow.Provider); ok {
		t.Fatal("read grants Capture")
	}
	if _, ok := v.(workflow.RestoreProvider); ok {
		t.Fatal("read grants Verify")
	}
}

func TestPackagesReadViewNewCycleDetectsAgain(t *testing.T) {
	p, d, r, _ := packageReadFixture(t)
	for range 2 {
		c := observation.New(context.Background())
		v := p.BindReadCycle(c)
		if _, e := v.Diff(context.Background(), d); e != nil {
			t.Fatal(e)
		}
		c.Close()
	}
	if r.detections != 2 {
		t.Fatal(r.detections)
	}
}

func TestPackagesReadPlanPreservesLiveScratchFields(t *testing.T) {
	p, d, r, _ := packageReadFixture(t)
	d.Packages.Official = []string{"dependency"}
	c := observation.New(context.Background())
	defer c.Close()
	v := p.BindReadCycle(c)
	targets, e := v.InspectTargets(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	rc := packageReadContext(targets)
	rc.Options.Convergence = policy.ConvergenceExact
	fragment, e := v.(workflow.ReadRestoreProvider).Plan(context.Background(), d, omarchy.Info{Version: "4.0.0"}, rc)
	if e != nil {
		t.Fatal(e)
	}
	for _, op := range fragment.Operations {
		if op.Action == "install" && op.Resource == "official:dependency" {
			t.Fatal("lost Installed fact caused needless install")
		}
	}
	if r.detections != 1 {
		t.Fatal(r.detections)
	}
	// Ensure stable output independently of projection ordering.
	again, e := v.(workflow.ReadRestoreProvider).Plan(context.Background(), d, omarchy.Info{Version: "4.0.0"}, rc)
	if e != nil || !reflect.DeepEqual(fragment, again) {
		t.Fatal("unstable repeated plan", e)
	}
}

func TestPackagesReadPlanUsesInstalledMiseAndSemanticFacts(t *testing.T) {
	p, d, r, _ := packageReadFixture(t)
	config := filepath.Join(t.TempDir(), "mise.toml")
	if e := os.WriteFile(config, []byte("[tools]\nnode = '24'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	p.deps.MiseGlobalConfig = func() (string, error) { return config, nil }
	r.responses = map[string]string{"mise ls --json": `{"node":[{"installed":true}]}`}
	recipe, _ := omarchy.SemanticRecipe("tailscale")
	for _, cmd := range recipe.Verify {
		r.responses[strings.Join(cmd, " ")] = ""
	}
	r.runner.official["tailscale"] = true
	d.Packages = profile.Packages{Official: []string{"dependency", "tailscale"}, Mise: profile.MiseTools{"node": {"version": "24"}}}
	c := observation.New(context.Background())
	defer c.Close()
	v := p.BindReadCycle(c).(*packagesReadView)
	targets, e := v.InspectTargets(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	rc := packageReadContext(targets)
	baseline, e := v.Plan(context.Background(), d, omarchy.Info{Version: "4.0.0"}, rc)
	if e != nil {
		t.Fatal(e)
	}
	f, e := v.slot.Get(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	facts := f.observed.Packages()
	if !facts.MiseInstalled["node"] || !facts.SemanticInstalled["tailscale"] || len(facts.SemanticRemoved) == 0 {
		t.Fatal("fixture lacks scratch maps")
	}
	for _, field := range []string{"Installed", "MiseInstalled", "SemanticInstalled"} {
		t.Run(field, func(t *testing.T) {
			current := f.observed.Packages()
			reflect.ValueOf(&current).Elem().FieldByName(field).SetZero()
			changed, e := planFromPackages(d, omarchy.Info{Version: "4.0.0"}, rc, f.detector, current)
			if e != nil {
				t.Fatal(e)
			}
			if reflect.DeepEqual(changed, baseline) {
				t.Fatalf("lost %s did not change plan; fixture does not protect live semantics", field)
			}
		})
	}
}

func TestPackagesReadOriginUnavailableParity(t *testing.T) {
	p, d, r, _ := packageReadFixture(t)
	r.runner.repos = []string{"core"}
	r.runner.dbPath = t.TempDir()
	c := observation.New(context.Background())
	defer c.Close()
	v := p.BindReadCycle(c).(*packagesReadView)
	got, e := v.InspectTargets(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	f, e := v.slot.Get(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	facts := f.observed.Packages()
	if !facts.OriginUnavailable || len(facts.MissingSyncDatabases) != 1 || len(facts.UnclassifiedExplicit) == 0 {
		t.Fatal("origin-unknown facts lost")
	}
	want, e := p.InspectTargets(context.Background(), d)
	if e != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("origin-unknown targets changed", e)
	}
	rc := packageReadContext(got)
	actual, e := v.Plan(context.Background(), d, omarchy.Info{Version: "4.0.0"}, rc)
	if e != nil {
		t.Fatal(e)
	}
	expected, e := p.Plan(context.Background(), d, omarchy.Info{Version: "4.0.0"}, rc)
	if e != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatal("origin-unknown preview changed", e)
	}
}

func TestPackagesVerifyDoesNotUseReadSnapshot(t *testing.T) {
	p, d, r, _ := packageReadFixture(t)
	d.Packages.Official = []string{"git"}
	ctx := context.Background()
	c := observation.New(ctx)
	defer c.Close()
	v := p.BindReadCycle(c)
	targets, e := v.InspectTargets(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	delete(r.runner.official, "git")
	result, e := p.Verify(ctx, d, packageReadContext(targets))
	if e != nil {
		t.Fatal(e)
	}
	if result.OK || r.detections != 2 {
		t.Fatal("Verify trusted read packages A after removal")
	}
	if _, e := v.Diff(ctx, d); e != nil {
		t.Fatal(e)
	}
	if r.detections != 2 {
		t.Fatal("Verify contaminated cycle")
	}
}
