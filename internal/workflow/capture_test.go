package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
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

func TestCaptureManyBuildsCompatibilityCaptureContextFromInspectedTargets(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	var order []string
	var lastCapture CaptureContext
	commits, rollbacks := 0, 0
	session.SetProviders([]Provider{
		captureTestProvider{
			id: "packages", order: &order, commits: &commits, rollbacks: &rollbacks,
			lastCapture: &lastCapture,
			targets:     []TargetInspection{{Key: "official:firefox"}, {Key: "official:neovim"}},
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
		if !ok || got != DefaultCaptureDecision() {
			t.Fatalf("Lookup(%q) = %+v, %v, want the PR 2 compatibility default", key, got, ok)
		}
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
