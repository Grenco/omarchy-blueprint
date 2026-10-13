package workflow

import (
	"context"
	"errors"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"reflect"
)

type SyncBootstrapChoice string
type SyncExecutionRequest struct {
	ReviewedPlan      syncengine.Plan
	Resolutions       []syncengine.TargetResolution
	Bootstrap         *SyncBootstrapChoice
	ReviewedBootstrap *SyncBootstrapPlan
	ApproveCapture    func(context.Context, CaptureInspection) error
	ApproveRestore    func(context.Context, model.RestorePlan) error
	Terminal          bool
	RestoreOptions    *policy.RestoreOptions
	ReviewActivation  func(context.Context, []model.ActivationCandidate) (*policy.ActivationReviewSelection, error)
}
type SyncExecutionResult struct {
	FinalRevision  string                 `json:"final_revision"`
	Captured       []syncengine.TargetKey `json:"captured,omitempty"`
	Restored       []syncengine.TargetKey `json:"restored,omitempty"`
	Published      bool                   `json:"published"`
	CursorAdvanced bool                   `json:"cursor_advanced"`
}

func (s *Session) ExecuteSync(ctx context.Context, req SyncExecutionRequest) (SyncExecutionResult, error) {
	result := SyncExecutionResult{}
	if req.Bootstrap != nil && *req.Bootstrap == "cancel" {
		return result, nil
	}
	state, err := s.deps.StateHome()
	if err != nil {
		return result, errors.New("Sync local state unavailable")
	}
	lock, err := syncengine.AcquireProfileLock(state, s.ProfileDir())
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := s.Reload(); err != nil {
		return result, errors.New("Sync execution profile is invalid")
	}
	var plan syncengine.Plan
	if req.Bootstrap != nil {
		if req.ReviewedBootstrap == nil || len(req.Resolutions) > 0 {
			return result, errors.New("bootstrap requires its own reviewed baseless inspection")
		}
		_, exists, loadErr := (syncengine.CursorStore{StateHome: state, ProfileRoot: s.ProfileDir()}).Load()
		if loadErr != nil || exists {
			return result, errors.New("bootstrap cannot replace an existing or invalid cursor")
		}
		fresh, err := s.InspectSyncBootstrap(ctx, SyncInspectOptions{})
		if err != nil {
			return result, err
		}
		if !reflect.DeepEqual(fresh, *req.ReviewedBootstrap) {
			return result, errors.New("bootstrap inspection changed; reinspect")
		}
		plan, err = ResolveSyncBootstrapPlan(fresh, *req.Bootstrap)
		if err != nil {
			return result, err
		}
	} else {
		plan, err = ResolveSyncPlan(req.ReviewedPlan, req.Resolutions)
		if err != nil {
			return result, err
		}
		fresh, err := s.InspectSync(ctx, SyncInspectOptions{Resolutions: syncPlanResolutions(plan)})
		if err != nil {
			return result, err
		}
		if !reflect.DeepEqual(plan, fresh) {
			return result, errors.New("Sync plan changed; reinspect and approve again")
		}
	}
	if len(plan.Blockers) > 0 || plan.BootstrapRequired {
		return result, errors.New("Sync plan is blocked; inspect before execution")
	}
	capture, follow := TargetSelection{}, TargetSelection{}
	for _, item := range plan.Items {
		if item.Deferred || item.Review != syncengine.ReviewNone {
			return result, errors.New("Sync has unresolved or deferred intent; no execution or cursor advancement")
		}
		for _, action := range item.Actions {
			switch action {
			case syncengine.ActionCapture:
				capture[item.Key.Category] = append(capture[item.Key.Category], item.Key.Target)
			case syncengine.ActionRestore:
				follow[item.Key.Category] = append(follow[item.Key.Category], item.Key.Target)
			}
		}
	}
	if len(capture) > 0 && req.ApproveCapture == nil {
		return result, errors.New("fresh Capture approval is required")
	}
	if len(follow) > 0 && req.ApproveRestore == nil {
		return result, errors.New("fresh Restore approval is required")
	}
	id := plan.Identity
	if plan.Git.Diverged {
		views := map[string]SyncProfileView{}
		cleanups := []func(){}
		defer func() {
			for _, cleanup := range cleanups {
				cleanup()
			}
		}()
		for _, sha := range []string{id.Base, id.Local, id.Remote} {
			v, cleanup, err := s.loadSyncProfileView(ctx, s.profileGit, sha)
			if err != nil {
				return result, err
			}
			views[sha] = v
			cleanups = append(cleanups, cleanup)
		}
		// Composition must use the same provider-owned semantic normalization
		// as inspection, not raw serialized aliases from the exact trees.
		cycle, err := s.BeginRead(ctx)
		if err != nil {
			return result, err
		}
		err = cycle.normalizeSyncViews(ctx, views, s.providers)
		cycle.Close() // no read cycle survives into mutation or human approval
		if err != nil {
			return result, err
		}
		expectedViews := map[string]map[syncengine.TargetKey]syncengine.State{id.Base: {}, id.Local: {}, id.Remote: {}}
		for _, item := range plan.Items {
			expectedViews[id.Base][item.Key] = item.Base
			expectedViews[id.Local][item.Key] = item.Local
			expectedViews[id.Remote][item.Key] = item.Remote
		}
		for _, sha := range []string{id.Base, id.Local, id.Remote} {
			actual, expected := syncTargetMap(views[sha].Targets), expectedViews[sha]
			for _, key := range syncKeys(actual, expected) {
				if !syncState(actual, key).Equal(syncState(expected, key)) {
					return result, errors.New("Sync normalization changed before composition; reinspect")
				}
			}
		}
		if plan.Control == nil || views[id.Base].Control.Fingerprint != plan.Control.Base || views[id.Local].Control.Fingerprint != plan.Control.Local || views[id.Remote].Control.Fingerprint != plan.Control.Remote {
			return result, errors.New("Sync control normalization changed before composition; reinspect")
		}
		resolutions := map[syncengine.TargetKey]syncengine.Resolution{}
		for _, item := range plan.Items {
			resolutions[item.Key] = item.Resolution
		}
		root, cleanup, err := s.composeSyncProfileTree(ctx, views[id.Base], views[id.Local], views[id.Remote], resolutions)
		if err != nil {
			return result, err
		}
		defer cleanup()
		_, err = s.profileGit.CommitSemanticMerge(ctx, profilegit.SemanticMergeRequest{BaseSHA: id.Base, LocalSHA: id.Local, RemoteSHA: id.Remote, StagingRoot: root})
		if err != nil {
			return result, err
		}
	} else if plan.Git.Behind > 0 {
		if _, err := s.profileGit.FastForwardTo(ctx, id.Local, id.Remote); err != nil {
			return result, err
		}
	}
	if err := s.Reload(); err != nil {
		return result, errors.New("reconciled profile cannot be reloaded")
	}
	if len(capture) > 0 {
		headBeforeCapture, err := s.profileGit.ResolveCommit(ctx, "HEAD")
		if err != nil {
			return result, err
		}
		review, err := s.InspectCaptureTargetsFresh(ctx, capture)
		if err != nil {
			return result, err
		}
		if err := req.ApproveCapture(ctx, review); err != nil {
			return result, err
		}
		if err := s.checkSyncExecutionIdentity(ctx, id, headBeforeCapture, state); err != nil {
			return result, err
		}
		if _, err := s.CaptureApprovedTargets(ctx, capture, review); err != nil {
			return result, err
		}
		if _, err := s.profileGit.Commit(ctx, "Capture accepted Sync machine intent"); err != nil {
			return result, errors.New("commit Sync Capture failed; inspect profile Git")
		}
		result.Captured = syncSelectionKeys(capture)
	}
	final, err := s.profileGit.ResolveCommit(ctx, "HEAD")
	if err != nil {
		return result, err
	}
	result.FinalRevision = final
	if err := s.profileGit.PublishSyncRevision(ctx, final, id.Branch, id.Upstream, id.RemoteFingerprint); err != nil {
		return result, err
	}
	result.Published = true
	if err := s.Reload(); err != nil {
		return result, err
	}
	if len(follow) > 0 {
		scope := RestoreScope{Targets: follow}
		approved, _, _, resolved, err := s.PlanRestoreScopeWithContext(ctx, scope, req.RestoreOptions)
		if err != nil {
			return result, err
		}
		if err := CheckRestoreApplicable(approved, req.Terminal); err != nil {
			return result, err
		}
		if resolved.ActivationMode() == policy.ActivationReview {
			if req.ReviewActivation == nil {
				return result, errors.New("interactive Services activation selection is required")
			}
			selection, err := req.ReviewActivation(ctx, append([]model.ActivationCandidate(nil), approved.ActivationReview...))
			if err != nil {
				return result, err
			}
			if selection == nil {
				return result, errors.New("Services activation review was not completed")
			}
			resolved.ReviewActivation = selection
			req.RestoreOptions = &resolved
			approved, _, _, _, err = s.PlanRestoreScopeWithContext(ctx, scope, req.RestoreOptions)
			if err != nil {
				return result, err
			}
			if err := CheckRestoreApplicable(approved, req.Terminal); err != nil {
				return result, err
			}
		}
		if err := req.ApproveRestore(ctx, approved); err != nil {
			return result, err
		}
		if err := s.checkSyncExecutionIdentity(ctx, id, final, state); err != nil {
			return result, err
		}
		applied, err := s.ApplyApprovedRestoreScope(ctx, scope, req.RestoreOptions, approved, req.Terminal)
		if err != nil {
			return result, err
		}
		if !applied.Verification.OK || len(applied.Plan.Deferred) > 0 || len(applied.Execution.SkippedByYou) > 0 || len(applied.Execution.Blocked) > 0 || len(applied.Execution.Failed) > 0 {
			return result, errors.New("Restore did not settle all selected Sync work")
		}
		result.Restored = syncSelectionKeys(follow)
	}
	if err := s.verifySyncSettlement(ctx, plan, final); err != nil {
		return result, err
	}
	if err := s.checkSyncExecutionIdentity(ctx, id, final, state); err != nil {
		return result, err
	}
	store := syncengine.CursorStore{StateHome: state, ProfileRoot: s.ProfileDir()}
	cursor, ok, err := store.Load()
	if !syncCursorMatches(cursor, ok, err, id) {
		return result, errors.New("Sync cursor changed during execution; settlement was not recorded")
	}
	if id.Base == "" {
		cursor = syncengine.Cursor{Version: syncengine.CursorFormatVersion, Machine: id.Machine, Branch: id.Branch, Upstream: id.Upstream, RemoteFingerprint: id.RemoteFingerprint}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	cursor.Revision = final
	return saveSyncExecutionCursor(result, cursor, store.Save)
}

// The final storage gate is deliberately separate from authority checks: an
// I/O error after verified settlement must still report CursorAdvanced=false.
func saveSyncExecutionCursor(result SyncExecutionResult, cursor syncengine.Cursor, save func(syncengine.Cursor) error) (SyncExecutionResult, error) {
	result.CursorAdvanced = false
	if err := save(cursor); err != nil {
		return result, errors.New("Sync settled but cursor save failed; settlement was not recorded")
	}
	result.CursorAdvanced = true
	return result, nil
}

func (s *Session) checkSyncExecutionIdentity(ctx context.Context, id syncengine.Identity, head, state string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := s.profileGit.CurrentRevisionInfo(ctx)
	if err != nil {
		return err
	}
	if info.SHA != head || info.Branch != id.Branch || info.Upstream != id.Upstream || info.OriginFingerprint != id.RemoteFingerprint {
		return errors.New("Sync authority changed after review; reinspect")
	}
	status, err := s.profileGit.ReadOnlyStatus(ctx)
	if err != nil || len(status.Changes) > 0 {
		return errors.New("Sync checkout changed after review")
	}
	if err := s.Reload(); err != nil || s.machine.Name != id.Machine {
		return errors.New("Sync machine selection changed after review")
	}
	cursor, ok, err := (syncengine.CursorStore{StateHome: state, ProfileRoot: s.ProfileDir()}).Load()
	if !syncCursorMatches(cursor, ok, err, id) {
		return errors.New("Sync cursor changed after review")
	}
	return nil
}

func syncCursorMatches(cursor syncengine.Cursor, exists bool, err error, id syncengine.Identity) bool {
	if err != nil {
		return false
	}
	if id.Base == "" {
		return !exists
	}
	return exists && cursor.Revision == id.Base && cursor.Machine == id.Machine && cursor.Branch == id.Branch && cursor.Upstream == id.Upstream && cursor.RemoteFingerprint == id.RemoteFingerprint
}

func ResolveSyncBootstrapPlan(p SyncBootstrapPlan, choice SyncBootstrapChoice) (syncengine.Plan, error) {
	if choice != "reconcile-upstream" && choice != "adopt-machine" {
		return syncengine.Plan{}, errors.New("unknown Sync bootstrap choice")
	}
	if len(p.Blockers) > 0 || !p.Git.Clean || p.Git.Diverged {
		return syncengine.Plan{}, errors.New("bootstrap is blocked; divergent baseless history requires external Git reconciliation")
	}
	plan := syncengine.Plan{Identity: p.Identity, Git: p.Git}
	for _, x := range p.Items {
		item := syncengine.Item{TargetInput: syncengine.TargetInput{Key: x.Key, Local: x.Local, Remote: x.Remote, Machine: x.Machine, Policy: x.Policy, Resolution: syncengine.ResolutionNone, MachineResolution: syncengine.MachineResolutionNone}, Desired: x.Remote, Review: syncengine.ReviewNone, Inapplicable: x.Inapplicable, Settled: x.Converged || x.Inapplicable}
		if choice == "reconcile-upstream" && p.Git.Ahead > 0 && !x.Local.Equal(x.Remote) {
			return syncengine.Plan{}, errors.New("baseless local committed intent differs; reconcile Git externally or explicitly adopt machine intent")
		}
		if choice == "adopt-machine" {
			item.Desired = x.Machine
			if !x.Machine.Equal(x.Remote) || !x.Machine.Equal(x.Local) {
				if !x.Policy.Capture {
					return syncengine.Plan{}, errors.New("bootstrap adoption cannot bypass Capture policy")
				}
				item.Actions = []syncengine.PlannedAction{syncengine.ActionCapture, syncengine.ActionPublish}
				item.Settled = false
			}
		} else if !x.Converged && !x.Inapplicable {
			if x.Remote.Kind == syncengine.StateUnmanaged && x.Policy.Capture {
				item.Desired = x.Machine
				item.Actions = []syncengine.PlannedAction{syncengine.ActionCapture, syncengine.ActionPublish}
			} else if x.Policy.Restore && x.Remote.Kind != syncengine.StateUnmanaged {
				item.Actions = []syncengine.PlannedAction{syncengine.ActionRestore}
			} else {
				item.Inapplicable = true
				item.Settled = true
			}
		}
		plan.Items = append(plan.Items, item)
	}
	return plan, nil
}

func ResolveSyncPlan(plan syncengine.Plan, choices []syncengine.TargetResolution) (syncengine.Plan, error) {
	byKey := map[syncengine.TargetKey]syncengine.TargetResolution{}
	for _, choice := range choices {
		if _, duplicate := byKey[choice.Key]; duplicate {
			return syncengine.Plan{}, errors.New("duplicate Sync resolution")
		}
		byKey[choice.Key] = choice
	}
	inputs := make([]syncengine.TargetInput, 0, len(plan.Items))
	for _, item := range plan.Items {
		input := item.TargetInput
		if choice, ok := byKey[item.Key]; ok {
			if choice.Resolution == syncengine.ResolutionDefer && item.History != syncengine.HistoryConflict {
				choice.Resolution = syncengine.ResolutionNone
				choice.MachineResolution = syncengine.MachineResolutionDefer
			}
			input.Resolution = choice.Resolution
			input.MachineResolution = choice.MachineResolution
			delete(byKey, item.Key)
		}
		inputs = append(inputs, input)
	}
	if len(byKey) > 0 {
		return syncengine.Plan{}, errors.New("unknown Sync resolution target")
	}
	resolved, err := syncengine.Reconcile(syncengine.PlanRequest{Identity: plan.Identity, Git: plan.Git, Targets: inputs})
	if err != nil {
		return syncengine.Plan{}, err
	}
	resolved.Control = plan.Control
	resolved.Blockers = plan.Blockers
	resolved.BootstrapRequired = plan.BootstrapRequired
	return resolved, nil
}
func syncPlanResolutions(plan syncengine.Plan) []syncengine.TargetResolution {
	choices := []syncengine.TargetResolution{}
	for _, item := range plan.Items {
		if item.Resolution != syncengine.ResolutionNone || item.MachineResolution != syncengine.MachineResolutionNone {
			choices = append(choices, syncengine.TargetResolution{Key: item.Key, Resolution: item.Resolution, MachineResolution: item.MachineResolution})
		}
	}
	return choices
}
func syncSelectionKeys(selection TargetSelection) []syncengine.TargetKey {
	keys := map[syncengine.TargetKey]syncengine.State{}
	for id, targets := range selection {
		for _, target := range targets {
			keys[syncengine.TargetKey{Category: id, Target: target}] = syncengine.State{}
		}
	}
	return syncKeys(keys)
}

func (s *Session) verifySyncSettlement(ctx context.Context, plan syncengine.Plan, final string) error {
	r, err := s.BeginRead(ctx)
	if err != nil {
		return err
	}
	defer r.Close()
	info, err := r.profileGit.CurrentRevisionInfo(ctx)
	if err != nil {
		return err
	}
	id := plan.Identity
	if info.SHA != final || info.Branch != id.Branch || info.Upstream != id.Upstream || info.OriginFingerprint != id.RemoteFingerprint || r.machine.Name != id.Machine {
		return errors.New("Sync execution identity changed before settlement")
	}
	status, err := r.profileGit.ReadOnlyStatus(ctx)
	if err != nil || len(status.Changes) > 0 {
		return errors.New("Sync checkout is not clean at settlement")
	}
	upstream, err := r.profileGit.UpstreamCommit(ctx)
	if err != nil {
		return err
	}
	reachable, err := r.profileGit.IsAncestor(ctx, final, upstream)
	if err != nil || !reachable {
		return errors.New("Sync final revision is not reachable upstream")
	}
	v, cleanup, err := s.loadSyncProfileView(ctx, r.profileGit, final)
	if err != nil {
		return err
	}
	defer cleanup()
	views := map[string]SyncProfileView{final: v}
	if err := r.normalizeSyncViews(ctx, views, s.providers); err != nil {
		return err
	}
	v = views[final]
	live, err := r.observeSync(ctx, v.Profile, []profile.Data{v.Profile})
	if err != nil {
		return err
	}
	if len(live.Blockers) > 0 {
		return errors.New("Sync settlement observation blocked")
	}
	desired, machine := syncTargetMap(v.Targets), syncTargetMap(live.Targets)
	inspections := map[syncengine.TargetKey]TargetInspection{}
	for category, targets := range live.Inspections {
		for _, target := range targets {
			inspections[syncengine.TargetKey{Category: category, Target: target.Key}] = target
		}
	}
	reviewed := map[syncengine.TargetKey]syncengine.Item{}
	for _, item := range plan.Items {
		reviewed[item.Key] = item
		captured := false
		for _, action := range item.Actions {
			captured = captured || action == syncengine.ActionCapture
		}
		// Capture's freshly approved fingerprint/staging/recheck boundary owns
		// new absence provenance and current values. A descriptive M fingerprint
		// is not Capture authority, nor necessarily the persisted tombstone hash.
		if !captured && !syncState(desired, item.Key).Equal(item.Desired) {
			return errors.New("Sync final desired state differs from reviewed intent")
		}
	}
	for _, key := range syncKeys(desired, machine) {
		observed, ok := machine[key]
		if !ok {
			return errors.New("Sync settlement lacks live target evidence")
		}
		if observed.Equal(syncState(desired, key)) {
			continue
		}
		item, ok := reviewed[key]
		if !ok || !item.Inapplicable {
			return errors.New("Sync machine still has applicable unsettled targets")
		}
		target, ok := inspections[key]
		if !ok {
			return errors.New("Sync settlement lacks policy evidence")
		}
		policy, err := effectivePolicy(v.Profile, PolicyScope{Machine: id.Machine}, key.Category, target)
		if err != nil {
			return err
		}
		if policy.Capture.Enabled != item.Policy.Capture || policy.Restore.Enabled != item.Policy.Restore {
			return fmt.Errorf("Sync settlement policy changed")
		}
	}
	return r.check(ctx)
}
