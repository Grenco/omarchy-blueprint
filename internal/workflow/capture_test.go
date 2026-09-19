package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type captureRunner struct{}

func (captureRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if name != "omarchy" || len(args) == 0 || args[0] != "version" {
		return "", errors.New("unexpected command")
	}
	if len(args) == 2 && args[1] == "channel" {
		return "stable", nil
	}
	return "4.0.0", nil
}

type captureTestProvider struct {
	id                            string
	order                         *[]string
	fail                          bool
	commitFail                    bool
	finalizeFail                  bool
	commits, rollbacks, finalizes *int
	targets                       []TargetInspection
	lastCapture                   *CaptureContext
	lastPlan                      *RestoreContext
	lastVerify                    *RestoreContext
	stopManagingFail              bool
}

func (p captureTestProvider) ID() string               { return p.id }
func (captureTestProvider) Captured(profile.Data) bool { return true }
func (captureTestProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return nil, nil
}
func (p captureTestProvider) InspectTargets(context.Context, profile.Data) ([]TargetInspection, error) {
	return p.targets, nil
}
func (p captureTestProvider) Capture(_ context.Context, data *profile.Data, capCtx CaptureContext) (any, []model.Change, error) {
	if p.lastCapture != nil {
		*p.lastCapture = capCtx
	}
	*p.order = append(*p.order, p.id)
	if p.fail {
		return nil, nil, errors.New("capture failed")
	}
	data.Packages.Official = append(data.Packages.Official, p.id)
	return struct{}{}, nil, nil
}
func (p captureTestProvider) Plan(_ context.Context, _ profile.Data, _ omarchy.Info, restoreCtx RestoreContext) (model.RestorePlan, error) {
	if p.lastPlan != nil {
		*p.lastPlan = restoreCtx
	}
	return model.RestorePlan{}, nil
}
func (p captureTestProvider) Verify(_ context.Context, _ profile.Data, restoreCtx RestoreContext) (model.VerificationResult, error) {
	if p.lastVerify != nil {
		*p.lastVerify = restoreCtx
	}
	return model.VerificationResult{OK: true}, nil
}
func (p captureTestProvider) StopManaging(_ context.Context, data profile.Data, target string) (profile.Data, error) {
	if p.stopManagingFail {
		return profile.Data{}, errors.New("stop managing rejected")
	}
	var kept []string
	for _, ref := range data.Packages.Official {
		if "official:"+ref != target {
			kept = append(kept, ref)
		}
	}
	data.Packages.Official = kept
	return data, nil
}
func (p captureTestProvider) CommitCapture() error {
	*p.commits++
	if p.commitFail {
		return errors.New("commit failed")
	}
	return nil
}

func TestCaptureManyRollsBackTransactionsAndProfileOnCommitFailure(t *testing.T) {
	data := profile.New("test", time.Now())
	session := newCaptureSession(t, data)
	var order []string
	commits, rollbacks := 0, 0
	session.SetProviders([]Provider{
		captureTestProvider{id: "packages", order: &order, commits: &commits, rollbacks: &rollbacks},
		captureTestProvider{id: "themes", order: &order, commitFail: true, commits: &commits, rollbacks: &rollbacks},
	})
	if _, err := session.CaptureMany(context.Background(), []string{"packages", "themes"}); err == nil {
		t.Fatal("capture succeeded")
	}
	if commits != 2 || rollbacks != 2 {
		t.Fatalf("commits=%d rollbacks=%d", commits, rollbacks)
	}
	loaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Packages.Official) != 0 || len(session.Profile().Packages.Official) != 0 {
		t.Fatalf("failed commit persisted profile: disk=%#v session=%#v", loaded.Packages, session.Profile().Packages)
	}
}
func (p captureTestProvider) FinalizeCapture() error {
	if p.finalizes != nil {
		*p.finalizes++
	}
	if p.finalizeFail {
		return errors.New("cleanup failed")
	}
	return nil
}
func (p captureTestProvider) RollbackCapture() error {
	*p.rollbacks++
	return nil
}

func TestCaptureManyReturnsCommittedResultWithCleanupWarning(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	var order []string
	commits, rollbacks, finalizes := 0, 0, 0
	session.SetProviders([]Provider{
		captureTestProvider{id: "packages", order: &order, commits: &commits, rollbacks: &rollbacks, finalizes: &finalizes, finalizeFail: true},
		captureTestProvider{id: "themes", order: &order, commits: &commits, rollbacks: &rollbacks, finalizes: &finalizes},
	})
	result, err := session.CaptureMany(context.Background(), []string{"packages", "themes"})
	var warning PostCommitWarning
	if !errors.As(err, &warning) || len(result.Providers) != 2 || finalizes != 2 || rollbacks != 0 {
		t.Fatalf("result=%#v err=%v finalizes=%d rollbacks=%d", result, err, finalizes, rollbacks)
	}
	loaded, loadErr := profile.Load(session.ProfileDir())
	if loadErr != nil || len(loaded.Packages.Official) != 2 || len(session.Profile().Packages.Official) != 2 {
		t.Fatalf("capture was not committed: disk=%#v session=%#v err=%v", loaded.Packages, session.Profile().Packages, loadErr)
	}
}

func newCaptureSession(t *testing.T, data profile.Data) *Session {
	t.Helper()
	profileDir, stateHome := t.TempDir(), t.TempDir()
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{Runner: captureRunner{}, Now: func() time.Time { return time.Unix(1, 0) }, StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestCaptureManyUsesConfiguredOrderAndPreservesExclusions(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Excluded = []string{"official:excluded"}
	session := newCaptureSession(t, data)
	var order []string
	commits, rollbacks := 0, 0
	session.SetProviders([]Provider{
		captureTestProvider{id: "packages", order: &order, commits: &commits, rollbacks: &rollbacks},
		captureTestProvider{id: "themes", order: &order, commits: &commits, rollbacks: &rollbacks},
	})

	result, err := session.CaptureMany(context.Background(), []string{"themes", "packages"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := order, []string{"packages", "themes"}; !sameStrings(got, want) {
		t.Fatalf("capture order=%v, want %v", got, want)
	}
	if got, want := result.Providers, []string{"packages", "themes"}; !sameStrings(got, want) {
		t.Fatalf("affected providers=%v, want %v", got, want)
	}
	if commits != 2 || rollbacks != 0 {
		t.Fatalf("commits=%d rollbacks=%d", commits, rollbacks)
	}
	loaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(loaded.Packages.Official, []string{"packages", "themes"}) || !sameStrings(loaded.Packages.Excluded, []string{"official:excluded"}) {
		t.Fatalf("saved packages=%#v", loaded.Packages)
	}
}

func TestCaptureManyDoesNotPersistPartialStateOnProviderFailure(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Excluded = []string{"official:excluded"}
	session := newCaptureSession(t, data)
	var order []string
	commits, rollbacks := 0, 0
	session.SetProviders([]Provider{
		captureTestProvider{id: "packages", order: &order, commits: &commits, rollbacks: &rollbacks},
		captureTestProvider{id: "themes", order: &order, fail: true, commits: &commits, rollbacks: &rollbacks},
	})

	if _, err := session.CaptureMany(context.Background(), []string{"packages", "themes"}); err == nil {
		t.Fatal("capture succeeded")
	}
	if commits != 0 || rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d", commits, rollbacks)
	}
	loaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Packages.Official) != 0 || !sameStrings(loaded.Packages.Excluded, []string{"official:excluded"}) {
		t.Fatalf("partial profile persisted: %#v", loaded.Packages)
	}
}

func TestCaptureManyResolvesRealPolicyForEveryInspectedTarget(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	var order []string
	var lastCapture CaptureContext
	commits, rollbacks := 0, 0
	session.SetProviders([]Provider{
		captureTestProvider{
			id: "packages", order: &order, commits: &commits, rollbacks: &rollbacks,
			lastCapture: &lastCapture,
			targets: []TargetInspection{
				{Key: "official:firefox", CaptureEligible: true},
				{Key: "official:neovim", CaptureEligible: true},
			},
		},
	})
	if _, err := session.Capture(context.Background(), "packages"); err != nil {
		t.Fatal(err)
	}
	if len(lastCapture.Targets) != 2 {
		t.Fatalf("targets = %#v, want 2 inspected targets recorded", lastCapture.Targets)
	}
	for _, key := range []string{"official:firefox", "official:neovim"} {
		got, ok := lastCapture.Lookup(key)
		want := CaptureDecision{Capture: true, Resolved: true}
		if !ok || got != want {
			t.Fatalf("Lookup(%q) = %+v, %v, want %+v: no policy rules exist, so an eligible target resolves to the built-in enabled default", key, got, ok, want)
		}
	}
}

func TestCaptureManyBlocksIneligibleTargetRegardlessOfPolicy(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	var order []string
	var lastCapture CaptureContext
	commits, rollbacks := 0, 0
	session.SetProviders([]Provider{
		captureTestProvider{
			id: "packages", order: &order, commits: &commits, rollbacks: &rollbacks,
			lastCapture: &lastCapture,
			targets:     []TargetInspection{{Key: "official:nvidia-utils", CaptureEligible: false}},
		},
	})
	if _, err := session.Capture(context.Background(), "packages"); err != nil {
		t.Fatal(err)
	}
	got, ok := lastCapture.Lookup("official:nvidia-utils")
	if !ok || got.Capture {
		t.Fatalf("Lookup(...) = %+v, %v, want CaptureEligible: false to be blocked regardless of policy", got, ok)
	}
}

func TestCaptureAllCapturesAllConfiguredProviders(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	var order []string
	commits, rollbacks := 0, 0
	session.SetProviders([]Provider{
		captureTestProvider{id: "packages", order: &order, commits: &commits, rollbacks: &rollbacks},
		captureTestProvider{id: "themes", order: &order, commits: &commits, rollbacks: &rollbacks},
	})

	result, err := session.Capture(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(order, []string{"packages", "themes"}) || !sameStrings(result.Providers, []string{"packages", "themes"}) {
		t.Fatalf("order=%v providers=%v", order, result.Providers)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// policyAwareTestProvider simulates a provider that actually consults its
// CaptureDecision: it clears the saved target when Capture is resolved
// enabled (simulating a merge that would otherwise remove a now-absent
// item), and leaves saved state untouched when Capture is disabled or
// unresolved (Preserve).
type policyAwareTestProvider struct {
	id      string
	key     string
	targets []TargetInspection
}

func (p policyAwareTestProvider) ID() string               { return p.id }
func (policyAwareTestProvider) Captured(profile.Data) bool { return true }
func (p policyAwareTestProvider) InspectTargets(context.Context, profile.Data) ([]TargetInspection, error) {
	return p.targets, nil
}
func (p policyAwareTestProvider) Capture(_ context.Context, data *profile.Data, capCtx CaptureContext) (any, []model.Change, error) {
	if decision, ok := capCtx.Lookup(p.key); ok && decision.Capture {
		data.Packages.Official = nil
	}
	return struct{}{}, nil, nil
}
func (policyAwareTestProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return nil, nil
}

// This is Task 16's headline regression: a target saved as present, now
// absent on this machine, with Capture Disabled for it on this machine, must
// remain present in the saved profile after Capture -- Capture Disabled
// means preserve, not delete, and provider safety/policy resolution must
// actually reach the provider as a real disabled decision, not the PR 2
// always-enabled compatibility stub.
func TestCaptureManyPreservesSavedTargetWhenMachineHasCaptureDisabled(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Official = []string{"firefox"}
	data.Machines.Items = []profile.Machine{{
		Name:   "desktop",
		Policy: policy.Rules{Capture: []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingDisabled}}},
	}}
	profileDir, stateHome := t.TempDir(), t.TempDir()
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{
		Runner: captureRunner{}, Now: func() time.Time { return time.Unix(1, 0) },
		StateHome: func() (string, error) { return stateHome, nil },
	}, Options{ProfileDir: profileDir, ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	session.SetProviders([]Provider{policyAwareTestProvider{
		id: "packages", key: "official:firefox",
		targets: []TargetInspection{{Key: "official:firefox", CaptureEligible: true, Current: TargetAbsent, Desired: TargetPresent}},
	}})

	if _, err := session.Capture(context.Background(), "packages"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(reloaded.Packages.Official, []string{"firefox"}) {
		t.Fatalf("official = %#v, want firefox preserved: Capture Disabled on this machine must not remove desired-present state", reloaded.Packages.Official)
	}
}

// desiredAbsenceTestProvider simulates a provider whose merge logic forms a
// desired-absence tombstone when a target that was desired present is now
// live-absent and Capture resolves enabled, mirroring the real Capture merge
// invariant table's "present + absent + enabled -> tombstone" row (proven at
// the provider level for Packages/Themes/Plugins/Hooks in Tasks 17-20).
type desiredAbsenceTestProvider struct {
	id      string
	key     string
	targets []TargetInspection
}

func (p desiredAbsenceTestProvider) ID() string               { return p.id }
func (desiredAbsenceTestProvider) Captured(profile.Data) bool { return true }
func (p desiredAbsenceTestProvider) InspectTargets(context.Context, profile.Data) ([]TargetInspection, error) {
	return p.targets, nil
}
func (p desiredAbsenceTestProvider) Capture(_ context.Context, data *profile.Data, capCtx CaptureContext) (any, []model.Change, error) {
	if decision, ok := capCtx.Lookup(p.key); ok && decision.Capture {
		data.Packages.Official = nil
		data.Packages.Absent = append(data.Packages.Absent, profile.PackageAbsence{Ref: p.key})
	}
	return struct{}{}, nil, nil
}
func (desiredAbsenceTestProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return nil, nil
}

// TestCaptureManyFormsDesiredAbsenceTombstoneWhenEnabledAndTargetMissing is
// Task 23's "desired absence" scenario proven at the session/orchestration
// level: a target saved present, now live-absent, with Capture resolved
// enabled (the built-in default; no disabling rule exists), must reach the
// provider as a real enabled decision that forms a tombstone rather than
// silently vanishing with no trace.
func TestCaptureManyFormsDesiredAbsenceTombstoneWhenEnabledAndTargetMissing(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Official = []string{"discord"}
	session := newCaptureSession(t, data)
	session.SetProviders([]Provider{desiredAbsenceTestProvider{
		id: "packages", key: "official:discord",
		targets: []TargetInspection{{Key: "official:discord", CaptureEligible: true, Current: TargetAbsent, Desired: TargetPresent}},
	}})

	if _, err := session.Capture(context.Background(), "packages"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Packages.Official) != 0 {
		t.Fatalf("official = %#v, want discord no longer desired-present", reloaded.Packages.Official)
	}
	if len(reloaded.Packages.Absent) != 1 || reloaded.Packages.Absent[0].Ref != "official:discord" {
		t.Fatalf("absent = %#v, want a tombstone for official:discord", reloaded.Packages.Absent)
	}
}

// adoptingTestProvider simulates a provider that adopts a live-present
// target into desired state only when Capture resolves enabled for it,
// mirroring the real "unknown + present + disabled -> remain unmanaged" row
// of the Capture merge invariant table.
type adoptingTestProvider struct {
	id      string
	key     string
	name    string
	targets []TargetInspection
}

func (p adoptingTestProvider) ID() string               { return p.id }
func (adoptingTestProvider) Captured(profile.Data) bool { return true }
func (p adoptingTestProvider) InspectTargets(context.Context, profile.Data) ([]TargetInspection, error) {
	return p.targets, nil
}
func (p adoptingTestProvider) Capture(_ context.Context, data *profile.Data, capCtx CaptureContext) (any, []model.Change, error) {
	if decision, ok := capCtx.Lookup(p.key); ok && decision.Capture {
		data.Packages.Official = append(data.Packages.Official, p.name)
	}
	return struct{}{}, nil, nil
}
func (adoptingTestProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return nil, nil
}

// TestCaptureManyNeverAdoptsNewTargetWhenDisabledFromTheStart is Task 23's
// "new-disabled" scenario: a target never captured before (Desired:
// Unknown), live-present, with Capture Disabled on this machine, must stay
// entirely unmanaged after Capture -- disabled never adopts new state, it
// only ever preserves what was already desired.
func TestCaptureManyNeverAdoptsNewTargetWhenDisabledFromTheStart(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{
		Name:   "desktop",
		Policy: policy.Rules{Capture: []policy.Rule{{Category: "packages", Target: "official:steam", Setting: policy.SettingDisabled}}},
	}}
	profileDir, stateHome := t.TempDir(), t.TempDir()
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{
		Runner: captureRunner{}, Now: func() time.Time { return time.Unix(1, 0) },
		StateHome: func() (string, error) { return stateHome, nil },
	}, Options{ProfileDir: profileDir, ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	session.SetProviders([]Provider{adoptingTestProvider{
		id: "packages", key: "official:steam", name: "steam",
		targets: []TargetInspection{{Key: "official:steam", CaptureEligible: true, Current: TargetPresent, Desired: TargetUnknown}},
	}})

	if _, err := session.Capture(context.Background(), "packages"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Packages.Official) != 0 {
		t.Fatalf("official = %#v, want steam never adopted while Capture is disabled", reloaded.Packages.Official)
	}
}
