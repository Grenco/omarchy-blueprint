package workflow

import (
	"context"
	"errors"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type executionSyncProvider struct {
	syncLiveFake
	ledger                  *[]string
	restoreTo               profile.Data
	failCapture, failVerify bool
	interactiveRestore      bool
}

func (p *executionSyncProvider) InspectTargets(ctx context.Context, d profile.Data) ([]TargetInspection, error) {
	items, err := p.SyncDesired(d)
	if err != nil {
		return nil, err
	}
	live, _ := p.SyncDesired(p.live)
	keys := syncKeys(syncTargetMap(items), syncTargetMap(live))
	out := []TargetInspection{}
	for _, key := range keys {
		out = append(out, TargetInspection{Key: key.Target, Current: TargetPresent, Desired: TargetPresent, CaptureEligible: true, RestoreEligible: true, Fingerprint: syncState(syncTargetMap(live), key).Fingerprint})
	}
	return out, nil
}
func (p *executionSyncProvider) Capture(_ context.Context, d *profile.Data, cc CaptureContext) (any, []model.Change, error) {
	*p.ledger = append(*p.ledger, "capture")
	if p.failCapture {
		return nil, nil, errors.New("injected Capture failure")
	}
	set := map[string]bool{}
	for _, name := range d.Packages.Official {
		set[name] = true
	}
	live := map[string]bool{}
	for _, name := range p.live.Packages.Official {
		live[name] = true
	}
	for key, decision := range cc.Targets {
		if decision.Capture && strings.HasPrefix(key, "official:") {
			name := strings.TrimPrefix(key, "official:")
			set[name] = live[name]
		}
	}
	d.Packages.Official = nil
	for name, present := range set {
		if present {
			d.Packages.Official = append(d.Packages.Official, name)
		}
	}
	return struct{}{}, nil, nil
}
func (p *executionSyncProvider) Plan(_ context.Context, d profile.Data, _ omarchy.Info, rc RestoreContext) (RestoreFragment, error) {
	*p.ledger = append(*p.ledger, "restore-plan")
	p.restoreTo = profile.CloneData(d)
	part := RestoreFragment{Compatibility: model.CompatibilityCategory{Category: "packages", Applies: rc.Applies(), Authority: model.CompatibilityUnchanged}}
	if rc.Applies() {
		part.Compatibility.State = model.CompatibilitySupported
		part.Compatibility.Evidence = []model.CompatibilityEvidence{{Kind: "fixture", Summary: "disposable fixture"}}
	}
	for key, decision := range rc.Targets {
		if decision.Restore {
			part.Operations = append(part.Operations, model.Operation{ID: "fixture:" + key, Provider: "packages", Resource: key, Action: "fixture", Command: []string{"fixture-restore", key}, Interactive: p.interactiveRestore})
		}
	}
	// Context maps may iterate arbitrarily; production providers also own stable ordering.
	for i := 0; i < len(part.Operations); i++ {
		for j := i + 1; j < len(part.Operations); j++ {
			if part.Operations[j].ID < part.Operations[i].ID {
				part.Operations[i], part.Operations[j] = part.Operations[j], part.Operations[i]
			}
		}
	}
	return part, nil
}
func (p *executionSyncProvider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	return []string{op.Resource}, true
}
func (p *executionSyncProvider) Verify(context.Context, profile.Data, RestoreContext) (model.VerificationResult, error) {
	*p.ledger = append(*p.ledger, "verify")
	return model.VerificationResult{OK: !p.failVerify}, nil
}

type executionSyncRunner struct {
	p           *executionSyncProvider
	failPush    bool
	skipRestore bool
}

func (r *executionSyncRunner) RunInteractive(ctx context.Context, name string, args ...string) error {
	if r.skipRestore {
		*r.p.ledger = append(*r.p.ledger, "restore-skip")
		return command.ErrSkippedByUser
	}
	_, err := r.Run(ctx, name, args...)
	return err
}

func (r *executionSyncRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "omarchy" {
		return (captureRunner{}).Run(ctx, name, args...)
	}
	if name == "fixture-restore" {
		*r.p.ledger = append(*r.p.ledger, "restore-apply")
		key := strings.TrimPrefix(args[0], "official:")
		set := map[string]bool{}
		for _, name := range r.p.live.Packages.Official {
			set[name] = true
		}
		set[key] = false
		for _, name := range r.p.restoreTo.Packages.Official {
			if name == key {
				set[key] = true
			}
		}
		r.p.live.Packages.Official = nil
		for name, present := range set {
			if present {
				r.p.live.Packages.Official = append(r.p.live.Packages.Official, name)
			}
		}
		return "", nil
	}
	if name == "git" {
		for _, a := range args {
			if a == "push" {
				*r.p.ledger = append(*r.p.ledger, "push")
				if r.failPush {
					return "", errors.New("injected push failure")
				}
			}
		}
	}
	return (command.SystemRunner{}).Run(ctx, name, args...)
}
func executionSyncFixture(t *testing.T) (*Session, *executionSyncProvider, *executionSyncRunner, profile.Data, string) {
	t.Helper()
	s, _, d, _, peer, _, _ := syncTwoClones(t)
	p := &executionSyncProvider{syncLiveFake: syncLiveFake{syncPackageProvider: syncPackageProvider{statusTestProvider{id: "packages"}}, live: profile.CloneData(d), binds: &atomic.Int32{}, observes: &atomic.Int32{}}, ledger: &[]string{}}
	r := &executionSyncRunner{p: p}
	s.deps.Runner = r
	s.deps.Now = time.Now
	s.profileGit.Runner = r
	s.SetProviders([]Provider{p})
	return s, p, r, d, peer
}
func syncCursorForTest(t *testing.T, s *Session) syncengine.Cursor {
	t.Helper()
	state, _ := s.deps.StateHome()
	c, ok, err := (syncengine.CursorStore{StateHome: state, ProfileRoot: s.ProfileDir()}).Load()
	if err != nil || !ok {
		t.Fatalf("cursor: %v", err)
	}
	return c
}

func TestSyncExecuteConvergedAdvancesOnlyVerifiedCursor(t *testing.T) {
	s, _, _, _, _ := executionSyncFixture(t)
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan})
	if err != nil || !result.CursorAdvanced || !result.Published || syncCursorForTest(t, s).Revision != plan.Identity.Local {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}
func TestSyncExecuteRemoteFollowPublishesBeforeFreshApprovedRestore(t *testing.T) {
	s, p, _, d, peer := executionSyncFixture(t)
	d.Packages.Official = append(d.Packages.Official, "remote")
	if err := profile.Save(peer, d); err != nil {
		t.Fatal(err)
	}
	syncGit(t, peer, "add", ".")
	syncGit(t, peer, "commit", "-m", "remote")
	syncGit(t, peer, "push")
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan, ApproveRestore: func(context.Context, model.RestorePlan) error {
		*p.ledger = append(*p.ledger, "approve-restore")
		return nil
	}})
	if err != nil || !result.CursorAdvanced {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	want := []string{"push", "restore-plan", "approve-restore", "restore-plan", "restore-apply", "verify"}
	if !reflect.DeepEqual(*p.ledger, want) {
		t.Fatalf("mutation order=%v want=%v", *p.ledger, want)
	}
	if syncCursorForTest(t, s).Revision != plan.Identity.Remote {
		t.Fatal("cursor recorded wrong revision")
	}
}
func TestSyncExecutePushFailureCannotRestoreOrAdvance(t *testing.T) {
	s, p, r, d, peer := executionSyncFixture(t)
	before := syncCursorForTest(t, s)
	d.Packages.Official = append(d.Packages.Official, "remote")
	profile.Save(peer, d)
	syncGit(t, peer, "add", ".")
	syncGit(t, peer, "commit", "-m", "remote")
	syncGit(t, peer, "push")
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	r.failPush = true
	result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan, ApproveRestore: func(context.Context, model.RestorePlan) error { return nil }})
	if err == nil || result.CursorAdvanced || !reflect.DeepEqual(before, syncCursorForTest(t, s)) {
		t.Fatalf("failed publication settled: %#v %v", result, err)
	}
	if !reflect.DeepEqual(*p.ledger, []string{"push"}) {
		t.Fatalf("Restore before publication: %v", *p.ledger)
	}
}

func TestSyncExecuteStaleAfterRestoreApprovalCannotMutateMachine(t *testing.T) {
	s, p, _, d, peer := executionSyncFixture(t)
	d.Packages.Official = append(d.Packages.Official, "remote")
	profile.Save(peer, d)
	syncGit(t, peer, "add", ".")
	syncGit(t, peer, "commit", "-m", "remote")
	syncGit(t, peer, "push")
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan, ApproveRestore: func(context.Context, model.RestorePlan) error {
		syncGit(t, s.ProfileDir(), "commit", "--allow-empty", "-m", "external HEAD change")
		return nil
	}})
	if err == nil {
		t.Fatal("accepted changed execution revision")
	}
	for _, stage := range *p.ledger {
		if stage == "restore-apply" {
			t.Fatal("mutated machine after execution authority changed")
		}
	}
}

func TestSyncExecuteBootstrapConvergedIsNotArbitraryMarkHead(t *testing.T) {
	s, p, _, d, _ := executionSyncFixture(t)
	state, _ := s.deps.StateHome()
	store := syncengine.CursorStore{StateHome: state, ProfileRoot: s.ProfileDir()}
	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
	review, err := s.InspectSyncBootstrap(context.Background(), SyncInspectOptions{})
	if err != nil || !review.ConvergenceEligible {
		t.Fatalf("bootstrap: %#v %v", review, err)
	}
	choice := SyncBootstrapChoice("reconcile-upstream")
	result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{Bootstrap: &choice, ReviewedBootstrap: &review})
	if err != nil || !result.CursorAdvanced {
		t.Fatalf("bootstrap: %#v %v", result, err)
	}
	store.Remove()
	review, _ = s.InspectSyncBootstrap(context.Background(), SyncInspectOptions{})
	p.live = profile.CloneData(d)
	p.live.Packages.Official = append(p.live.Packages.Official, "new-drift")
	result, err = s.ExecuteSync(context.Background(), SyncExecutionRequest{Bootstrap: &choice, ReviewedBootstrap: &review})
	if err == nil || result.CursorAdvanced {
		t.Fatal("bootstrap ignored fresh drift")
	}
	if _, ok, _ := store.Load(); ok {
		t.Fatal("stale bootstrap wrote cursor")
	}
}

func TestSyncExecuteFailureMatrixLeavesCursorBehind(t *testing.T) {
	for _, failure := range []string{"stale-head", "dirty", "stale-cursor", "machine-changed", "restore-verify", "cursor-invalidated", "push-race"} {
		t.Run(failure, func(t *testing.T) {
			s, p, _, d, peer := executionSyncFixture(t)
			before := syncCursorForTest(t, s)
			d.Packages.Official = append(d.Packages.Official, "remote")
			profile.Save(peer, d)
			syncGit(t, peer, "add", ".")
			syncGit(t, peer, "commit", "-m", "remote")
			syncGit(t, peer, "push")
			plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
			if err != nil {
				t.Fatal(err)
			}
			state, _ := s.deps.StateHome()
			store := syncengine.CursorStore{StateHome: state, ProfileRoot: s.ProfileDir()}
			approve := func(context.Context, model.RestorePlan) error { return nil }
			switch failure {
			case "stale-head":
				syncGit(t, s.ProfileDir(), "commit", "--allow-empty", "-m", "changed")
			case "dirty":
				os.WriteFile(filepath.Join(s.ProfileDir(), "untracked"), []byte("user"), 0600)
			case "stale-cursor":
				changed := before
				changed.Revision = plan.Identity.Remote
				store.Save(changed)
			case "machine-changed":
				s.opts.ExplicitMachine = "other"
			case "restore-verify":
				p.failVerify = true
			case "cursor-invalidated":
				approve = func(context.Context, model.RestorePlan) error {
					dir, _ := machine.ProfileStateDir(state, s.ProfileDir())
					path := filepath.Join(dir, "sync.toml")
					os.Remove(path)
					os.Mkdir(path, 0700)
					return nil
				}
			case "push-race":
				d.Packages.Official = append(d.Packages.Official, "not-reviewed")
				profile.Save(peer, d)
				syncGit(t, peer, "add", ".")
				syncGit(t, peer, "commit", "-m", "later")
				syncGit(t, peer, "push")
			}
			result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan, ApproveRestore: approve})
			if err == nil || result.CursorAdvanced {
				t.Fatalf("failure claimed settlement: %#v %v", result, err)
			}
			if failure != "cursor-invalidated" && failure != "stale-cursor" && !reflect.DeepEqual(before, syncCursorForTest(t, s)) {
				t.Fatal("failure advanced cursor")
			}
		})
	}
}

func TestSyncExecuteMixedRemoteAndMachineIntentKeepsStageOrder(t *testing.T) {
	s, p, _, d, peer := executionSyncFixture(t)
	p.live.Packages.Official = append(p.live.Packages.Official, "machine-new")
	d.Packages.Official = append(d.Packages.Official, "remote")
	profile.Save(peer, d)
	syncGit(t, peer, "add", ".")
	syncGit(t, peer, "commit", "-m", "remote")
	syncGit(t, peer, "push")
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan,
		ApproveCapture: func(context.Context, CaptureInspection) error {
			*p.ledger = append(*p.ledger, "approve-capture")
			return nil
		},
		ApproveRestore: func(context.Context, model.RestorePlan) error {
			*p.ledger = append(*p.ledger, "approve-restore")
			return nil
		},
	})
	if err != nil || !result.CursorAdvanced || len(result.Captured) != 1 || len(result.Restored) != 1 {
		t.Fatalf("mixed reconciliation: %#v %v", result, err)
	}
	want := []string{"approve-capture", "capture", "push", "restore-plan", "approve-restore", "restore-plan", "restore-apply", "verify"}
	if !reflect.DeepEqual(*p.ledger, want) {
		t.Fatalf("stage order=%v want=%v", *p.ledger, want)
	}
	if syncGit(t, s.ProfileDir(), "rev-parse", "HEAD^") != plan.Identity.Remote {
		t.Fatal("Capture not based on reviewed remote history")
	}
}

func TestSyncExecuteCursorWriteFailureDoesNotClaimSettlement(t *testing.T) {
	final := strings.Repeat("a", 40)
	result := SyncExecutionResult{FinalRevision: final, Published: true}
	cursor := syncengine.Cursor{Revision: final}
	called := false
	result, err := saveSyncExecutionCursor(result, cursor, func(c syncengine.Cursor) error {
		called = true
		if c.Revision != final {
			t.Fatal("attempted to settle unreviewed revision")
		}
		return os.ErrPermission
	})
	if !called || err == nil || result.CursorAdvanced || !result.Published || !strings.Contains(err.Error(), "cursor save failed") {
		t.Fatalf("storage failure hidden: %#v %v", result, err)
	}
}

func TestSyncExecuteLocalPublishAndDivergentHistory(t *testing.T) {
	for _, divergent := range []bool{false, true} {
		t.Run(map[bool]string{false: "local-publish", true: "divergent"}[divergent], func(t *testing.T) {
			s, p, _, d, peer := executionSyncFixture(t)
			left := profile.CloneData(d)
			left.Packages.Official = append(left.Packages.Official, "local")
			profile.Save(s.ProfileDir(), left)
			syncGit(t, s.ProfileDir(), "add", ".")
			syncGit(t, s.ProfileDir(), "commit", "-m", "local")
			p.live = profile.CloneData(left)
			if divergent {
				right := profile.CloneData(d)
				right.Packages.Official = append(right.Packages.Official, "remote")
				profile.Save(peer, right)
				syncGit(t, peer, "add", ".")
				syncGit(t, peer, "commit", "-m", "remote")
				syncGit(t, peer, "push")
			}
			plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan, ApproveRestore: func(context.Context, model.RestorePlan) error { return nil }})
			if err != nil || !result.CursorAdvanced {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if divergent && syncGit(t, s.ProfileDir(), "show", "-s", "--format=%P", "HEAD") != plan.Identity.Local+" "+plan.Identity.Remote {
				t.Fatal("semantic histories lost")
			}
			if !divergent && result.FinalRevision != plan.Identity.Local {
				t.Fatal("publish rewrote local history")
			}
		})
	}
}

func TestSyncExecuteKeepMachineUsesFreshSelectedCapture(t *testing.T) {
	s, p, _, d, _ := executionSyncFixture(t)
	p.live = profile.CloneData(d)
	p.live.Packages.Official = append(p.live.Packages.Official, "machine-new")
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan, ApproveCapture: func(context.Context, CaptureInspection) error {
		*p.ledger = append(*p.ledger, "approve-capture")
		return nil
	}})
	if err != nil || !result.CursorAdvanced || len(result.Captured) != 1 || result.Captured[0].Target != "official:machine-new" {
		t.Fatalf("capture=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(*p.ledger, []string{"approve-capture", "capture", "push"}) {
		t.Fatalf("order=%v", *p.ledger)
	}
}

func TestSyncExecuteLiveChangeInvalidatesReadPlanBeforeMutation(t *testing.T) {
	s, p, _, d, _ := executionSyncFixture(t)
	p.live.Packages.Official = append(p.live.Packages.Official, "state-a")
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p.live = profile.CloneData(d)
	p.live.Packages.Official = append(p.live.Packages.Official, "state-b")
	_, err = s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan, ApproveCapture: func(context.Context, CaptureInspection) error { return nil }})
	if err == nil || len(*p.ledger) != 0 {
		t.Fatal("read observation became stale Capture authority")
	}
}

func TestSyncExecuteDeferDoesNotMutateOrSettle(t *testing.T) {
	s, p, _, d, _ := executionSyncFixture(t)
	p.live.Packages.Official = []string{"z"}
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var choices []syncengine.TargetResolution
	for _, item := range plan.Items {
		if item.Review == syncengine.ReviewMachine {
			choices = append(choices, syncengine.TargetResolution{Key: item.Key, MachineResolution: syncengine.MachineResolutionDefer})
		}
	}
	if len(choices) == 0 {
		t.Fatal("fixture did not create machine conflict")
	}
	before := syncCursorForTest(t, s)
	_, err = s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan, Resolutions: choices})
	if err == nil || len(*p.ledger) != 0 || !reflect.DeepEqual(before, syncCursorForTest(t, s)) {
		t.Fatal("deferred intent was settled")
	}
	_ = d
}

func TestSyncExecuteCaptureFreshApprovalInvalidationAndFailure(t *testing.T) {
	for _, failure := range []string{"capture", "changed-after-approval", "cancelled-approval"} {
		t.Run(failure, func(t *testing.T) {
			s, p, _, _, _ := executionSyncFixture(t)
			p.live.Packages.Official = append(p.live.Packages.Official, "machine-new")
			plan, err := s.InspectSync(context.Background(), SyncInspectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			before := syncCursorForTest(t, s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			state, _ := s.deps.StateHome()
			approve := func(context.Context, CaptureInspection) error {
				if lock, err := syncengine.AcquireProfileLock(state, s.ProfileDir()); err == nil {
					lock.Close()
					t.Fatal("execution did not hold exclusive Sync lock during approval")
				}
				switch failure {
				case "capture":
					p.failCapture = true
				case "changed-after-approval":
					p.live.Packages.Official = append(p.live.Packages.Official, "not-approved")
				case "cancelled-approval":
					cancel()
				}
				return nil
			}
			result, err := s.ExecuteSync(ctx, SyncExecutionRequest{ReviewedPlan: plan, ApproveCapture: approve})
			if err == nil || result.Published || result.CursorAdvanced || !reflect.DeepEqual(before, syncCursorForTest(t, s)) {
				t.Fatalf("failed Capture settled: %#v %v", result, err)
			}
			if got := syncGit(t, s.ProfileDir(), "status", "--porcelain"); got != "" {
				t.Fatal("failed Capture left profile changes")
			}
			lock, err := syncengine.AcquireProfileLock(state, s.ProfileDir())
			if err != nil {
				t.Fatalf("failed execution leaked lock: %v", err)
			}
			lock.Close()
		})
	}
}

func TestSyncExecuteSkippedRestoreDoesNotSettle(t *testing.T) {
	s, p, r, d, peer := executionSyncFixture(t)
	d.Packages.Official = append(d.Packages.Official, "remote")
	profile.Save(peer, d)
	syncGit(t, peer, "add", ".")
	syncGit(t, peer, "commit", "-m", "remote")
	syncGit(t, peer, "push")
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	before := syncCursorForTest(t, s)
	p.interactiveRestore, r.skipRestore = true, true
	result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan, Terminal: true, ApproveRestore: func(context.Context, model.RestorePlan) error { return nil }})
	if err == nil || !result.Published || result.CursorAdvanced || !reflect.DeepEqual(before, syncCursorForTest(t, s)) {
		t.Fatalf("skipped Restore settled: %#v %v", result, err)
	}
}

type normalizingExecutionProvider struct{ *executionSyncProvider }
type normalizingExecutionRead struct{ *syncLiveFakeRead }

func (p normalizingExecutionProvider) BindReadCycle(c *observation.Cycle) ReadProvider {
	return normalizingExecutionRead{p.syncLiveFake.BindReadCycle(c).(*syncLiveFakeRead)}
}
func (normalizingExecutionRead) NormalizeSyncProfile(_ context.Context, d profile.Data) (profile.Data, error) {
	for i, name := range d.Packages.Official {
		if name == "legacy" {
			d.Packages.Official[i] = "canonical"
		}
	}
	return d, nil
}
func TestSyncExecuteDivergentCompositionUsesProviderNormalization(t *testing.T) {
	s, p, _, d, peer := executionSyncFixture(t)
	if err := s.SetProviders([]Provider{normalizingExecutionProvider{p}}); err != nil {
		t.Fatal(err)
	}
	left := profile.CloneData(d)
	left.Packages.Official = append(left.Packages.Official, "legacy")
	profile.Save(s.ProfileDir(), left)
	syncGit(t, s.ProfileDir(), "add", ".")
	syncGit(t, s.ProfileDir(), "commit", "-m", "local legacy identity")
	p.live.Packages.Official = append(p.live.Packages.Official, "canonical")
	d.Packages.Official = append(d.Packages.Official, "remote")
	profile.Save(peer, d)
	syncGit(t, peer, "add", ".")
	syncGit(t, peer, "commit", "-m", "remote")
	syncGit(t, peer, "push")
	plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ExecuteSync(context.Background(), SyncExecutionRequest{ReviewedPlan: plan, ApproveRestore: func(context.Context, model.RestorePlan) error { return nil }})
	if err != nil || !result.CursorAdvanced {
		t.Fatalf("normalization failed: %#v %v", result, err)
	}
	loaded, err := profile.Load(s.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range loaded.Packages.Official {
		if name == "legacy" {
			t.Fatal("composition bypassed provider normalization")
		}
	}
}
